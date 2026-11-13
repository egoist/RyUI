// asq_wasm.c is what SQLite needs of an operating system when it's built
// to WebAssembly (SQLITE_OS_OTHER; internal/sqlitebuild's wasm target),
// each a function JavaScript gives the module (its imports, from "asq":
// platform/web/src/sqlite-engine.js): the files SQLite keeps databases
// and journals in, their locks, the time and the time zone, randomness.
//
// The module runs in a worker of the page (platform/web/src/sqlite-worker.js),
// one call at a time: SQLITE_THREADSAFE=0.

#include <string.h>
#include <time.h>

#include "sqlite3.h"

#define JS(name) __attribute__((import_module("asq"), import_name(#name)))

// open opens the file at path (NULL: a temporary one) with SQLite's open
// flags: its id, or minus an error code.
JS(open) extern int js_open(const char *path, int flags);
JS(close) extern int js_close(int id);
// read reads n bytes at off into p: SQLITE_OK, SQLITE_IOERR_SHORT_READ
// (the rest zeroed) or an error.
JS(read) extern int js_read(int id, void *p, int n, double off);
JS(write) extern int js_write(int id, const void *p, int n, double off);
JS(truncate) extern int js_truncate(int id, double size);
JS(sync) extern int js_sync(int id, int flags);
// size is the file's size, or -1.
JS(size) extern double js_size(int id);
JS(lock) extern int js_lock(int id, int level);
JS(unlock) extern int js_unlock(int id, int level);
JS(reserved) extern int js_reserved(int id);
JS(remove) extern int js_remove(const char *path, int syncDir);
JS(access) extern int js_access(const char *path, int flags);
JS(random) extern void js_random(void *p, int n);
// now is the time in milliseconds since 1970.
JS(now) extern double js_now(void);
// localtime fills v with t's (seconds since 1970) local time in the
// browser's time zone: seconds, minutes, hours, day of the month, month
// (0-11), year - 1900, day of the week, day of the year, daylight saving
// time, offset from UTC in seconds.
JS(localtime) extern void js_localtime(double t, int *v);

typedef struct {
  sqlite3_file base;
  int id;
} jsfile;

#define ID(f) (((jsfile *)(f))->id)

static int fClose(sqlite3_file *f) { return js_close(ID(f)); }

static int fRead(sqlite3_file *f, void *p, int n, sqlite3_int64 off) { return js_read(ID(f), p, n, (double)off); }

static int fWrite(sqlite3_file *f, const void *p, int n, sqlite3_int64 off) { return js_write(ID(f), p, n, (double)off); }

static int fTruncate(sqlite3_file *f, sqlite3_int64 size) { return js_truncate(ID(f), (double)size); }

static int fSync(sqlite3_file *f, int flags) { return js_sync(ID(f), flags); }

static int fFileSize(sqlite3_file *f, sqlite3_int64 *out) {
  double n = js_size(ID(f));
  if (n < 0) return SQLITE_IOERR_FSTAT;
  *out = (sqlite3_int64)n;
  return SQLITE_OK;
}

static int fLock(sqlite3_file *f, int level) { return js_lock(ID(f), level); }

static int fUnlock(sqlite3_file *f, int level) { return js_unlock(ID(f), level); }

static int fCheckReservedLock(sqlite3_file *f, int *out) {
  *out = js_reserved(ID(f));
  return SQLITE_OK;
}

static int fFileControl(sqlite3_file *f, int op, void *arg) {
  (void)f, (void)op, (void)arg;
  return SQLITE_NOTFOUND;
}

static int fSectorSize(sqlite3_file *f) {
  (void)f;
  return 4096;
}

static int fDeviceCharacteristics(sqlite3_file *f) {
  (void)f;
  return SQLITE_IOCAP_UNDELETABLE_WHEN_OPEN;
}

// Version 1: no shared memory, so no write-ahead logging; the rollback
// journal.
static const sqlite3_io_methods methods = {
    1,
    fClose,
    fRead,
    fWrite,
    fTruncate,
    fSync,
    fFileSize,
    fLock,
    fUnlock,
    fCheckReservedLock,
    fFileControl,
    fSectorSize,
    fDeviceCharacteristics,
};

static int vOpen(sqlite3_vfs *v, sqlite3_filename name, sqlite3_file *f, int flags, int *outFlags) {
  (void)v;
  f->pMethods = NULL;
  int id = js_open(name, flags);
  if (id < 0) return -id;
  ((jsfile *)f)->id = id;
  f->pMethods = &methods;
  if (outFlags) *outFlags = flags;
  return SQLITE_OK;
}

static int vDelete(sqlite3_vfs *v, const char *name, int syncDir) {
  (void)v;
  return js_remove(name, syncDir);
}

static int vAccess(sqlite3_vfs *v, const char *name, int flags, int *out) {
  (void)v;
  *out = js_access(name, flags);
  return SQLITE_OK;
}

// Paths come from Go whole (store.Dir's, or absolute).
static int vFullPathname(sqlite3_vfs *v, const char *name, int n, char *out) {
  (void)v;
  size_t len = strlen(name);
  int slash = name[0] != '/';
  if ((int)(len + slash) >= n) return SQLITE_CANTOPEN;
  if (slash) out[0] = '/';
  memcpy(out + slash, name, len + 1);
  return SQLITE_OK;
}

static int vRandomness(sqlite3_vfs *v, int n, char *out) {
  (void)v;
  js_random(out, n);
  return n;
}

// The worker never sleeps: a connection that finds a database locked
// returns SQLITE_BUSY at once, and Go runs the statement again (its busy
// timeout is Go's).
static int vSleep(sqlite3_vfs *v, int us) {
  (void)v, (void)us;
  return 0;
}

static int vCurrentTime(sqlite3_vfs *v, double *out) {
  (void)v;
  *out = 2440587.5 + js_now() / 86400000.0;
  return SQLITE_OK;
}

static int vCurrentTimeInt64(sqlite3_vfs *v, sqlite3_int64 *out) {
  (void)v;
  *out = (sqlite3_int64)210866760000000LL + (sqlite3_int64)js_now();
  return SQLITE_OK;
}

static int vGetLastError(sqlite3_vfs *v, int n, char *out) {
  (void)v, (void)n, (void)out;
  return 0;
}

static sqlite3_vfs vfs = {
    2,
    sizeof(jsfile),
    1024,
    NULL,
    "asuka",
    NULL,
    vOpen,
    vDelete,
    vAccess,
    vFullPathname,
    NULL,
    NULL,
    NULL,
    NULL,
    vRandomness,
    vSleep,
    vCurrentTime,
    vGetLastError,
    vCurrentTimeInt64,
};

int sqlite3_os_init(void) { return sqlite3_vfs_register(&vfs, 1); }

int sqlite3_os_end(void) { return SQLITE_OK; }

// localtime_r is the browser's (HAVE_LOCALTIME_R): SQLite's 'localtime'
// and 'utc' modifiers then use the page's time zone, not UTC.
struct tm *localtime_r(const time_t *t, struct tm *tm) {
  int v[10];
  js_localtime((double)*t, v);
  memset(tm, 0, sizeof *tm);
  tm->tm_sec = v[0];
  tm->tm_min = v[1];
  tm->tm_hour = v[2];
  tm->tm_mday = v[3];
  tm->tm_mon = v[4];
  tm->tm_year = v[5];
  tm->tm_wday = v[6];
  tm->tm_yday = v[7];
  tm->tm_isdst = v[8];
  tm->tm_gmtoff = v[9];
  return tm;
}
