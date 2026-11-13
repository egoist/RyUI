// asq: see asq.h. Compiled with SQLite's amalgamation into one library
// (internal/sqlitebuild); nothing here is called but asq_call and
// asq_version.
//
// A connection is used by one thread at a time (Go's pool hands it out),
// but may move between threads from call to call: SQLite is built
// multi-thread (SQLITE_THREADSAFE=2), and nothing here is global but
// what's read only.

#include <stdlib.h>
#include <string.h>

#include "asq.h"
#include "sqlite3.h"

// --- Buffers ---

// wbuf writes an answer: into the caller's buffer while it fits, then
// into one of its own, which the connection keeps for TAKE.
typedef struct {
  uint8_t *b;
  size_t n, cap;
  int own; // b is malloc'd
  int oom;
} wbuf;

static int w_need(wbuf *w, size_t k) {
  if (w->oom) return 0;
  if (w->n + k <= w->cap) return 1;
  size_t cap = w->cap * 2;
  if (cap < w->n + k) cap = w->n + k;
  if (cap < 4096) cap = 4096;
  uint8_t *b = malloc(cap);
  if (!b) {
    w->oom = 1;
    return 0;
  }
  if (w->n) memcpy(b, w->b, w->n);
  if (w->own) free(w->b);
  w->b = b;
  w->cap = cap;
  w->own = 1;
  return 1;
}

static void w_u8(wbuf *w, uint8_t v) {
  if (w_need(w, 1)) w->b[w->n++] = v;
}

static void w_u32(wbuf *w, uint32_t v) {
  if (!w_need(w, 4)) return;
  uint8_t *p = w->b + w->n;
  p[0] = v, p[1] = v >> 8, p[2] = v >> 16, p[3] = v >> 24;
  w->n += 4;
}

static void w_u64(wbuf *w, uint64_t v) {
  w_u32(w, (uint32_t)v);
  w_u32(w, (uint32_t)(v >> 32));
}

static void w_raw(wbuf *w, const void *p, size_t n) {
  if (n && w_need(w, n)) {
    memcpy(w->b + w->n, p, n);
    w->n += n;
  }
}

static void w_bytes(wbuf *w, const void *p, size_t n) {
  w_u32(w, (uint32_t)n);
  w_raw(w, p, n);
}

static void w_str(wbuf *w, const char *s) { w_bytes(w, s, s ? strlen(s) : 0); }

static void w_set_u8(wbuf *w, size_t at, uint8_t v) {
  if (!w->oom) w->b[at] = v;
}

static void w_set_u32(wbuf *w, size_t at, uint32_t v) {
  if (w->oom) return;
  uint8_t *p = w->b + at;
  p[0] = v, p[1] = v >> 8, p[2] = v >> 16, p[3] = v >> 24;
}

// rbuf reads a call's input; reading past its end sets bad.
typedef struct {
  const uint8_t *p, *end;
  int bad;
} rbuf;

static int r_has(rbuf *r, size_t n) {
  if (r->bad || (size_t)(r->end - r->p) < n) {
    r->bad = 1;
    return 0;
  }
  return 1;
}

static uint8_t r_u8(rbuf *r) { return r_has(r, 1) ? *r->p++ : 0; }

static uint32_t r_u32(rbuf *r) {
  if (!r_has(r, 4)) return 0;
  const uint8_t *p = r->p;
  r->p += 4;
  return (uint32_t)p[0] | (uint32_t)p[1] << 8 | (uint32_t)p[2] << 16 | (uint32_t)p[3] << 24;
}

static uint64_t r_u64(rbuf *r) {
  uint64_t lo = r_u32(r);
  return lo | (uint64_t)r_u32(r) << 32;
}

static const uint8_t *r_bytes(rbuf *r, uint32_t *n) {
  *n = r_u32(r);
  if (!r_has(r, *n)) {
    *n = 0;
    return (const uint8_t *)"";
  }
  const uint8_t *p = r->p;
  r->p += *n;
  return p;
}

// --- Sets of table names ---

typedef struct {
  char **v;
  int n, cap;
} nameset;

static void set_add(nameset *s, const char *x) {
  for (int i = 0; i < s->n; i++)
    if (strcmp(s->v[i], x) == 0) return;
  if (s->n == s->cap) {
    int cap = s->cap ? s->cap * 2 : 4;
    char **v = realloc(s->v, cap * sizeof *v);
    if (!v) return;
    s->v = v;
    s->cap = cap;
  }
  size_t n = strlen(x) + 1;
  char *c = malloc(n);
  if (!c) return;
  memcpy(c, x, n);
  s->v[s->n++] = c;
}

static void set_union(nameset *dst, const nameset *src) {
  for (int i = 0; i < src->n; i++) set_add(dst, src->v[i]);
}

static void set_clear(nameset *s) {
  for (int i = 0; i < s->n; i++) free(s->v[i]);
  s->n = 0;
}

static void set_free(nameset *s) {
  set_clear(s);
  free(s->v);
  s->v = NULL;
  s->cap = 0;
}

static void w_set(wbuf *w, const nameset *s) {
  w_u32(w, s->n);
  for (int i = 0; i < s->n; i++) w_str(w, s->v[i]);
}

// --- Connections and statements ---

typedef struct conn conn;

// stmt is a prepared statement: one alone in its SQL is kept (cached) for
// the SQL's next run, with the tables it reads and writes, which SQLite's
// authorizer told as it prepared it and won't again.
typedef struct stmt {
  conn *c;
  sqlite3_stmt *s;
  char *sql; // a cached one's
  size_t sqlLen;
  uint64_t hash;
  int cache;  // alone in its SQL: kept once done
  int cursor; // a cursor has it: it gives it back
  nameset reads, writes;
  struct stmt *prev, *next; // among the idle, oldest first
} stmt;

typedef struct cursor {
  uint32_t id;
  stmt *s;
  int n;    // columns
  int step; // step before the next row (the first row is stepped)
  struct cursor *next;
} cursor;

#define MAX_CACHED 64

struct conn {
  sqlite3 *db;
  stmt *head, *tail; // idle statements, oldest first
  int cached;
  cursor *cursors;
  uint32_t nextCursor;
  nameset written;   // by the transaction going on
  nameset committed; // since the last answer
  nameset reads;     // the tables read, while recording
  int recording;
  stmt *preparing, *current;
  uint8_t *pending; // an answer too long for the caller's buffer
  size_t pendingLen;
};

static void stmt_free(stmt *s) {
  sqlite3_finalize(s->s);
  free(s->sql);
  set_free(&s->reads);
  set_free(&s->writes);
  free(s);
}

static void idle_unlink(conn *c, stmt *s) {
  if (s->prev) s->prev->next = s->next;
  else c->head = s->next;
  if (s->next) s->next->prev = s->prev;
  else c->tail = s->prev;
  s->prev = s->next = NULL;
  c->cached--;
}

// release resets s and keeps it for its SQL's next run (the oldest idle
// statement goes when there are too many), or finalizes a script's.
static void release(conn *c, stmt *s) {
  s->cursor = 0;
  if (!s->cache) {
    stmt_free(s);
    return;
  }
  sqlite3_reset(s->s);
  sqlite3_clear_bindings(s->s);
  if (c->cached >= MAX_CACHED) {
    stmt *old = c->head;
    idle_unlink(c, old);
    stmt_free(old);
  }
  s->prev = c->tail;
  s->next = NULL;
  if (c->tail) c->tail->next = s;
  else c->head = s;
  c->tail = s;
  c->cached++;
}

static uint64_t hash(const uint8_t *p, size_t n) {
  uint64_t h = 14695981039346656037ULL;
  for (size_t i = 0; i < n; i++) h = (h ^ p[i]) * 1099511628211ULL;
  return h;
}

// cache_take takes the newest idle statement of sql, noting its tables as
// preparing it again would.
static stmt *cache_take(conn *c, const uint8_t *sql, size_t n, uint64_t h) {
  for (stmt *s = c->tail; s; s = s->prev) {
    if (s->hash == h && s->sqlLen == n && memcmp(s->sql, sql, n) == 0) {
      idle_unlink(c, s);
      set_union(&c->written, &s->writes);
      if (c->recording) set_union(&c->reads, &s->reads);
      return s;
    }
  }
  return NULL;
}

// SQLite asks as it prepares a statement (and prepares it again after a
// schema change): the tables it reads and writes, a view's too, and a
// delete of every row, which SQLite may do without visiting them.
static int authorize(void *p, int code, const char *a, const char *b, const char *db, const char *trigger) {
  (void)b, (void)db, (void)trigger;
  conn *c = p;
  if (!a || strncmp(a, "sqlite_", 7) == 0) return SQLITE_OK;
  stmt *s = c->preparing ? c->preparing : c->current;
  switch (code) {
  case SQLITE_READ:
    if (s) set_add(&s->reads, a);
    if (c->recording) set_add(&c->reads, a);
    break;
  case SQLITE_INSERT:
  case SQLITE_UPDATE:
  case SQLITE_DELETE:
    if (s) set_add(&s->writes, a);
    set_add(&c->written, a);
    break;
  }
  return SQLITE_OK;
}

// Every row written, by triggers and foreign keys' actions too.
static void preupdate(void *p, sqlite3 *db, int op, const char *schema, const char *table, sqlite3_int64 a, sqlite3_int64 b) {
  (void)db, (void)op, (void)schema, (void)a, (void)b;
  set_add(&((conn *)p)->written, table);
}

static int on_commit(void *p) {
  conn *c = p;
  set_union(&c->committed, &c->written);
  set_clear(&c->written);
  return 0;
}

static void on_rollback(void *p) { set_clear(&((conn *)p)->written); }

// --- Calls ---

// call is a call's state: its answer, and its error.
typedef struct {
  conn *c;
  wbuf w;
  int rc;
  char *msg; // malloc'd; NULL: the connection's errmsg
  uintptr_t outCap;
  conn *opened; // OPEN's new connection
} call;

static int fail(call *k, int rc, const char *msg) {
  k->rc = rc;
  free(k->msg);
  k->msg = NULL;
  if (msg) {
    size_t n = strlen(msg) + 1;
    k->msg = malloc(n);
    if (k->msg) memcpy(k->msg, msg, n);
  } else if (k->c) {
    const char *m = sqlite3_errmsg(k->c->db);
    k->msg = malloc(strlen(m) + 1);
    if (k->msg) strcpy(k->msg, m);
  }
  return rc;
}

typedef struct {
  const uint8_t *name;
  uint32_t nameLen;
  uint8_t type;
  int64_t i;
  double f;
  const uint8_t *p;
  uint32_t n;
} arg;

typedef struct {
  arg *v;
  uint32_t n;
  arg small[16];
} args;

static int read_args(call *k, rbuf *r, args *a) {
  a->n = r_u32(r);
  a->v = a->small;
  if (a->n > 16) {
    if (a->n > (uint32_t)(r->end - r->p)) return fail(k, SQLITE_MISUSE, "asq: bad arguments");
    a->v = malloc(a->n * sizeof(arg));
    if (!a->v) return fail(k, SQLITE_NOMEM, "out of memory");
  }
  for (uint32_t i = 0; i < a->n; i++) {
    arg *x = &a->v[i];
    x->name = r_bytes(r, &x->nameLen);
    x->type = r_u8(r);
    switch (x->type) {
    case 1: x->i = (int64_t)r_u64(r); break;
    case 2: {
      uint64_t u = r_u64(r);
      memcpy(&x->f, &u, 8);
      break;
    }
    case 3:
    case 4: x->p = r_bytes(r, &x->n); break;
    }
  }
  if (r->bad) return fail(k, SQLITE_MISUSE, "asq: bad arguments");
  return SQLITE_OK;
}

static void free_args(args *a) {
  if (a->v != a->small) free(a->v);
}

static int digits(const char *s) {
  if (*s < '1' || *s > '9') return 0;
  for (; *s; s++)
    if (*s < '0' || *s > '9') return 0;
  return 1;
}

// bind binds a's to s's parameters: ?NNN, $NNN and the n-th by number,
// :name, @name and $name by name (the first argument of that name).
static int bind(call *k, stmt *s, args *a) {
  int n = sqlite3_bind_parameter_count(s->s);
  for (int i = 1; i <= n; i++) {
    const char *name = sqlite3_bind_parameter_name(s->s, i);
    long ord = i;
    if (name) {
      ord = 0;
      if ((name[0] == '?' || name[0] == '$') && digits(name + 1)) ord = strtol(name + 1, NULL, 10);
    }
    long j = ord >= 1 && ord <= (long)a->n ? ord - 1 : -1;
    if (name && name[1]) {
      size_t len = strlen(name + 1);
      for (uint32_t m = 0; m < a->n; m++) {
        if (a->v[m].nameLen == len && memcmp(a->v[m].name, name + 1, len) == 0) {
          if (j < 0 || (long)m < j) j = m;
          break;
        }
      }
    }
    if (j < 0) {
      char msg[256];
      if (name) sqlite3_snprintf(sizeof msg, msg, "missing named argument \"%s\"", name + 1);
      else sqlite3_snprintf(sizeof msg, msg, "missing argument with index %d", i);
      return fail(k, SQLITE_RANGE, msg);
    }
    arg *x = &a->v[j];
    int rc;
    switch (x->type) {
    case 1: rc = sqlite3_bind_int64(s->s, i, x->i); break;
    case 2: rc = sqlite3_bind_double(s->s, i, x->f); break;
    case 3: rc = sqlite3_bind_text(s->s, i, (const char *)x->p, (int)x->n, SQLITE_TRANSIENT); break;
    case 4: rc = sqlite3_bind_blob(s->s, i, x->p, (int)x->n, SQLITE_TRANSIENT); break;
    default: rc = sqlite3_bind_null(s->s, i);
    }
    if (rc) return fail(k, rc, NULL);
  }
  return SQLITE_OK;
}

static int step(conn *c, stmt *s) {
  c->current = s;
  int rc = sqlite3_step(s->s);
  c->current = NULL;
  return rc;
}

static int blank(const char *p, const char *end) {
  for (; p < end; p++)
    if (*p != ' ' && *p != '\t' && *p != '\n' && *p != '\r' && *p != ';') return 0;
  return 1;
}

// each is what statements does with each statement: SQLITE_OK for the
// next, SQLITE_DONE to stop, or an error.
typedef int (*each)(call *k, stmt *s, void *data);

// statements runs f on each statement of sql in turn (the one cached for
// it, or prepared), until f stops; each is released after, unless f gave
// it to a cursor, which releases it as it closes.
static int statements(call *k, const uint8_t *sql, size_t n, each f, void *data) {
  conn *c = k->c;
  uint64_t h = hash(sql, n);
  stmt *s = cache_take(c, sql, n, h);
  if (s) {
    int rc = f(k, s, data);
    if (!s->cursor) release(c, s);
    return rc == SQLITE_DONE ? SQLITE_OK : rc;
  }
  const char *at = (const char *)sql, *end = at + n;
  int first = 1;
  while (at < end) {
    s = calloc(1, sizeof *s);
    if (!s) return fail(k, SQLITE_NOMEM, "out of memory");
    s->c = c;
    const char *tail = NULL;
    c->preparing = s;
    int rc = sqlite3_prepare_v3(c->db, at, (int)(end - at), SQLITE_PREPARE_PERSISTENT, &s->s, &tail);
    c->preparing = NULL;
    if (rc) {
      fail(k, rc, NULL);
      stmt_free(s);
      return rc;
    }
    if (!s->s) { // only space or comments left
      stmt_free(s);
      break;
    }
    // A statement alone is cached; a script's are prepared each time.
    s->cache = first && blank(tail, end);
    if (s->cache) {
      s->sql = malloc(n ? n : 1);
      if (!s->sql) s->cache = 0;
      else {
        memcpy(s->sql, sql, n);
        s->sqlLen = n;
        s->hash = h;
      }
    }
    first = 0;
    at = tail;
    rc = f(k, s, data);
    if (!s->cursor) release(c, s);
    if (rc == SQLITE_DONE) return SQLITE_OK;
    if (rc) return rc;
  }
  return SQLITE_OK;
}

static int exec_one(call *k, stmt *s, void *data) {
  int rc = bind(k, s, data);
  if (rc) return rc;
  while ((rc = step(k->c, s)) == SQLITE_ROW) {
  }
  if (rc != SQLITE_DONE) return fail(k, rc, NULL);
  return SQLITE_OK;
}

static void w_columns(wbuf *w, sqlite3_stmt *s) {
  int n = sqlite3_column_count(s);
  w_u32(w, n);
  for (int i = 0; i < n; i++) {
    w_str(w, sqlite3_column_name(s, i));
    w_str(w, sqlite3_column_decltype(s, i));
  }
}

static void close_rows(conn *c, uint32_t id) {
  for (cursor **p = &c->cursors; *p; p = &(*p)->next) {
    if ((*p)->id == id) {
      cursor *cur = *p;
      *p = cur->next;
      release(c, cur->s);
      free(cur);
      return;
    }
  }
}

// fill writes the cursor's next rows: u8 done, u32 rows, then each row's
// values; as many as fit the caller's buffer (one at least). Its caller
// closes the cursor once done, or failed.
static int fill(call *k, cursor *cur, int *donep) {
  conn *c = k->c;
  wbuf *w = &k->w;
  sqlite3_stmt *s = cur->s->s;
  size_t at = w->n;
  w_u8(w, 0);
  w_u32(w, 0);
  // Room for the answer's tables after the rows.
  size_t budget = k->outCap > 1024 ? k->outCap - 512 : 0;
  uint32_t rows = 0;
  int done = 0;
  for (;;) {
    if (cur->step) {
      int rc = step(c, cur->s);
      if (rc == SQLITE_DONE) {
        done = 1;
        break;
      }
      if (rc != SQLITE_ROW) return fail(k, rc, NULL);
    }
    cur->step = 1;
    for (int i = 0; i < cur->n; i++) {
      switch (sqlite3_column_type(s, i)) {
      case SQLITE_NULL: w_u8(w, 0); break;
      case SQLITE_INTEGER:
        w_u8(w, 1);
        w_u64(w, (uint64_t)sqlite3_column_int64(s, i));
        break;
      case SQLITE_FLOAT: {
        double f = sqlite3_column_double(s, i);
        uint64_t u;
        memcpy(&u, &f, 8);
        w_u8(w, 2);
        w_u64(w, u);
        break;
      }
      case SQLITE_TEXT: {
        const unsigned char *p = sqlite3_column_text(s, i);
        w_u8(w, 3);
        w_bytes(w, p, sqlite3_column_bytes(s, i));
        break;
      }
      default: {
        const void *p = sqlite3_column_blob(s, i);
        w_u8(w, 4);
        w_bytes(w, p, sqlite3_column_bytes(s, i));
      }
      }
    }
    rows++;
    if (w->oom) return fail(k, SQLITE_NOMEM, "out of memory");
    if (w->own || w->n >= budget) break;
  }
  w_set_u8(w, at, done);
  w_set_u32(w, at + 1, rows);
  *donep = done;
  return SQLITE_OK;
}

typedef struct {
  args *a;
  int found;
  wbuf cols; // the first statement's columns, when none has rows
} query_data;

static int query_one(call *k, stmt *s, void *data) {
  query_data *q = data;
  int rc = bind(k, s, q->a);
  if (rc) return rc;
  rc = step(k->c, s);
  if (rc == SQLITE_ROW) {
    conn *c = k->c;
    cursor *cur = calloc(1, sizeof *cur);
    if (!cur) return fail(k, SQLITE_NOMEM, "out of memory");
    cur->id = ++c->nextCursor;
    if (!cur->id) cur->id = ++c->nextCursor;
    cur->s = s;
    cur->n = sqlite3_column_count(s->s);
    cur->next = c->cursors;
    c->cursors = cur;
    s->cursor = 1;
    q->found = 1;
    w_u32(&k->w, cur->id);
    w_columns(&k->w, s->s);
    int done = 0;
    rc = fill(k, cur, &done);
    if (rc || done) {
      // All its rows in the first answer (or none, failing): statements
      // releases the statement.
      c->cursors = cur->next;
      free(cur);
      s->cursor = 0;
    }
    return rc ? rc : SQLITE_DONE;
  }
  if (rc != SQLITE_DONE) return fail(k, rc, NULL);
  if (!q->cols.n) w_columns(&q->cols, s->s);
  return SQLITE_OK;
}

static cursor *find_cursor(conn *c, uint32_t id) {
  for (cursor *cur = c->cursors; cur; cur = cur->next)
    if (cur->id == id) return cur;
  return NULL;
}

static int serialize(call *k) {
  sqlite3_int64 n = 0;
  unsigned char *p = sqlite3_serialize(k->c->db, "main", &n, 0);
  if (!p && n) return fail(k, SQLITE_NOMEM, "out of memory");
  w_u64(&k->w, (uint64_t)n);
  w_raw(&k->w, p, (size_t)n);
  sqlite3_free(p);
  if (k->w.oom) return fail(k, SQLITE_NOMEM, "out of memory");
  return SQLITE_OK;
}

// restore replaces the main database with a serialized one, through
// SQLite's backup (a file stays a file, its journal mode its own).
static int restore(call *k, rbuf *r) {
  uint64_t n = r_u64(r);
  if (!r_has(r, n)) return fail(k, SQLITE_MISUSE, "asq: bad input");
  sqlite3 *src = NULL;
  int rc = sqlite3_open_v2(":memory:", &src, SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE | SQLITE_OPEN_MEMORY, NULL);
  if (rc) {
    sqlite3_close(src);
    return fail(k, rc, "can't open a database to restore from");
  }
  unsigned char *p = sqlite3_malloc64(n ? n : 1);
  if (!p) {
    sqlite3_close(src);
    return fail(k, SQLITE_NOMEM, "out of memory");
  }
  memcpy(p, r->p, n);
  // A database in write-ahead logging says so in its header (bytes 18
  // and 19), which a database in memory can't open: it's read as one in
  // a rollback journal, and the backup leaves the destination's mode.
  if (n >= 100 && p[18] == 2 && p[19] == 2) p[18] = p[19] = 1;
  rc = sqlite3_deserialize(src, "main", p, n, n, SQLITE_DESERIALIZE_FREEONCLOSE | SQLITE_DESERIALIZE_RESIZEABLE);
  if (rc) {
    sqlite3_close(src);
    return fail(k, rc, "not a database");
  }
  sqlite3_backup *b = sqlite3_backup_init(k->c->db, "main", src, "main");
  if (!b) {
    sqlite3_close(src);
    return fail(k, sqlite3_errcode(k->c->db), NULL);
  }
  rc = sqlite3_backup_step(b, -1);
  int fin = sqlite3_backup_finish(b);
  sqlite3_close(src);
  if (rc != SQLITE_DONE) return fail(k, rc, NULL);
  if (fin) return fail(k, fin, NULL);
  return SQLITE_OK;
}

static int open_conn(call *k, rbuf *r) {
  uint32_t busy = r_u32(r);
  uint32_t n;
  const uint8_t *path = r_bytes(r, &n);
  if (r->bad) return fail(k, SQLITE_MISUSE, "asq: bad input");
  char *z = malloc(n + 1);
  conn *c = calloc(1, sizeof *c);
  if (!z || !c) {
    free(z), free(c);
    return fail(k, SQLITE_NOMEM, "out of memory");
  }
  memcpy(z, path, n);
  z[n] = 0;
  int flags = SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE | SQLITE_OPEN_URI | SQLITE_OPEN_EXRESCODE;
  int rc = sqlite3_open_v2(z, &c->db, flags, NULL);
  free(z);
  if (rc) {
    const char *m = c->db ? sqlite3_errmsg(c->db) : sqlite3_errstr(rc);
    fail(k, rc, m);
    sqlite3_close_v2(c->db);
    free(c);
    return rc;
  }
  sqlite3_extended_result_codes(c->db, 1);
  sqlite3_busy_timeout(c->db, (int)busy);
  sqlite3_set_authorizer(c->db, authorize, c);
  sqlite3_preupdate_hook(c->db, preupdate, c);
  sqlite3_commit_hook(c->db, on_commit, c);
  sqlite3_rollback_hook(c->db, on_rollback, c);
  w_u64(&k->w, (uint64_t)(uintptr_t)c);
  k->opened = c;
  return SQLITE_OK;
}

static void close_conn(conn *c) {
  while (c->cursors) close_rows(c, c->cursors->id);
  while (c->head) {
    stmt *s = c->head;
    idle_unlink(c, s);
    stmt_free(s);
  }
  sqlite3_set_authorizer(c->db, NULL, NULL);
  sqlite3_preupdate_hook(c->db, NULL, NULL);
  sqlite3_commit_hook(c->db, NULL, NULL);
  sqlite3_rollback_hook(c->db, NULL, NULL);
  sqlite3_close_v2(c->db);
  set_free(&c->written);
  set_free(&c->committed);
  set_free(&c->reads);
  free(c->pending);
  free(c);
}

intptr_t asq_version(void) { return sqlite3_libversion_number(); }

intptr_t asq_call(uintptr_t op, uintptr_t connp, uintptr_t arg, const uint8_t *in, uintptr_t inLen, uint8_t *out, uintptr_t outCap) {
  conn *c = (conn *)connp;
  if (op == ASQ_INTERRUPT) {
    if (c) sqlite3_interrupt(c->db);
    return 0;
  }
  if (op == ASQ_TAKE) {
    if (!c || !c->pending) return 0;
    size_t n = c->pendingLen;
    if (n > outCap) return -(intptr_t)n;
    memcpy(out, c->pending, n);
    free(c->pending);
    c->pending = NULL;
    c->pendingLen = 0;
    return (intptr_t)n;
  }
  if (op == ASQ_CLOSE) {
    if (c) close_conn(c);
    if (outCap < 13) return 0;
    out[0] = 1;
    out[1] = 5, out[2] = out[3] = out[4] = 0;
    memset(out + 5, 0, 8);
    return 13;
  }

  call k = {.c = c, .w = {.b = out, .cap = out ? outCap : 0}, .outCap = outCap};
  rbuf r = {.p = in, .end = in + inLen};
  if (c) {
    free(c->pending);
    c->pending = NULL;
    c->pendingLen = 0;
  }
  w_u8(&k.w, 1);
  w_u32(&k.w, 0); // the tables' offset, below
  size_t body = k.w.n;
  int flags = op == ASQ_OPEN ? 0 : r_u8(&r);
  if (c) {
    c->recording = flags & ASQ_READS;
    set_clear(&c->reads);
  }

  int rc = SQLITE_OK;
  if (op != ASQ_OPEN && !c) rc = fail(&k, SQLITE_MISUSE, "sqlite: closed");
  else switch (op) {
  case ASQ_OPEN: rc = open_conn(&k, &r); break;
  case ASQ_EXEC: {
    uint32_t n;
    const uint8_t *sql = r_bytes(&r, &n);
    args a;
    rc = read_args(&k, &r, &a);
    if (rc) break;
    rc = statements(&k, sql, n, exec_one, &a);
    free_args(&a);
    if (!rc) {
      w_u64(&k.w, (uint64_t)sqlite3_last_insert_rowid(c->db));
      w_u64(&k.w, (uint64_t)sqlite3_changes64(c->db));
    }
    break;
  }
  case ASQ_QUERY: {
    uint32_t n;
    const uint8_t *sql = r_bytes(&r, &n);
    args a;
    rc = read_args(&k, &r, &a);
    if (rc) break;
    query_data q = {.a = &a};
    rc = statements(&k, sql, n, query_one, &q);
    free_args(&a);
    if (!rc && !q.found) {
      w_u32(&k.w, 0);
      if (q.cols.n) w_raw(&k.w, q.cols.b, q.cols.n);
      else w_u32(&k.w, 0);
      w_u8(&k.w, 1); // done
      w_u32(&k.w, 0); // no rows
    }
    if (q.cols.own) free(q.cols.b);
    break;
  }
  case ASQ_NEXT: {
    cursor *cur = find_cursor(c, (uint32_t)arg);
    int done = 0;
    rc = cur ? fill(&k, cur, &done) : fail(&k, SQLITE_MISUSE, "sqlite: rows closed");
    if (cur && (rc || done)) close_rows(c, cur->id);
    break;
  }
  case ASQ_CLOSE_ROWS: close_rows(c, (uint32_t)arg); break;
  case ASQ_SERIALIZE: rc = serialize(&k); break;
  case ASQ_RESTORE: rc = restore(&k, &r); break;
  default: rc = fail(&k, SQLITE_MISUSE, "asq: unknown op");
  }

  if (rc) {
    k.w.n = body;
    w_set_u8(&k.w, 0, 0);
    w_u32(&k.w, (uint32_t)k.rc);
    w_u8(&k.w, c && sqlite3_get_autocommit(c->db) ? 1 : 0);
    w_str(&k.w, k.msg ? k.msg : sqlite3_errstr(k.rc));
  }
  free(k.msg);
  // The tables the call's commits wrote, and those its queries read.
  w_set_u32(&k.w, 1, (uint32_t)k.w.n);
  if (c) {
    w_set(&k.w, &c->committed);
    set_clear(&c->committed);
    w_set(&k.w, &c->reads);
    set_clear(&c->reads);
    c->recording = 0;
  } else {
    w_u32(&k.w, 0);
    w_u32(&k.w, 0);
  }
  if (k.w.oom) {
    if (k.w.own) free(k.w.b);
    if (outCap < 64) return 0;
    // Out of memory: an answer that says so fits any buffer Go passes.
    wbuf w = {.b = out, .cap = outCap};
    w_u8(&w, 0);
    w_u32(&w, 0);
    w_u32(&w, SQLITE_NOMEM);
    w_u8(&w, c && sqlite3_get_autocommit(c->db));
    w_str(&w, "out of memory");
    w_set_u32(&w, 1, (uint32_t)w.n);
    w_u32(&w, 0);
    w_u32(&w, 0);
    return (intptr_t)w.n;
  }
  if (!k.w.own) return (intptr_t)k.w.n;
  // Too long for the caller's buffer: kept for TAKE.
  if (!c) {
    // OPEN's answer is short: only a buffer too small gets here. There's
    // no connection to keep it on: the new one closes, and the caller
    // opens again with a buffer that big.
    if (k.opened) close_conn(k.opened);
    intptr_t n = (intptr_t)k.w.n;
    free(k.w.b);
    return -n;
  }
  c->pending = k.w.b;
  c->pendingLen = k.w.n;
  return -(intptr_t)k.w.n;
}
