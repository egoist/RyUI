package sqlite

import "strconv"

// Error is an error SQLite returned: its result code (extended: 2067 is
// SQLITE_CONSTRAINT_UNIQUE), and its message.
//
// errors.Is matches it against the Err values by code: ErrConstraint
// matches every constraint's, ErrConstraintUnique only a unique one's.
//
//	if errors.Is(err, sqlite.ErrConstraintUnique) { ... }
type Error struct {
	Code    int
	Message string

	autocommit bool // the connection wasn't in a transaction after it
}

func (e *Error) Error() string {
	if e.Message == "" {
		return "sqlite: " + CodeName(e.Code)
	}
	return "sqlite: " + e.Message + " (" + CodeName(e.Code) + ")"
}

// Is matches an Err value: one with a primary code matches every code it
// extends.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok || t.Message != "" {
		return false
	}
	if t.Code == e.Code {
		return true
	}
	return t.Code <= 0xff && t.Code == e.Code&0xff
}

const (
	codeBusy   = 5
	codeMisuse = 21
)

// The errors to match with errors.Is.
var (
	ErrError                 = &Error{Code: 1}
	ErrInternal              = &Error{Code: 2}
	ErrPermission            = &Error{Code: 3}
	ErrAbort                 = &Error{Code: 4}
	ErrBusy                  = &Error{Code: 5}
	ErrLocked                = &Error{Code: 6}
	ErrNoMemory              = &Error{Code: 7}
	ErrReadOnly              = &Error{Code: 8}
	ErrInterrupt             = &Error{Code: 9}
	ErrIO                    = &Error{Code: 10}
	ErrCorrupt               = &Error{Code: 11}
	ErrFull                  = &Error{Code: 13}
	ErrCantOpen              = &Error{Code: 14}
	ErrSchema                = &Error{Code: 17}
	ErrTooBig                = &Error{Code: 18}
	ErrConstraint            = &Error{Code: 19}
	ErrMismatch              = &Error{Code: 20}
	ErrMisuse                = &Error{Code: 21}
	ErrRange                 = &Error{Code: 25}
	ErrNotADatabase          = &Error{Code: 26}
	ErrConstraintCheck       = &Error{Code: 275}
	ErrConstraintForeignKey  = &Error{Code: 787}
	ErrConstraintNotNull     = &Error{Code: 1299}
	ErrConstraintPrimaryKey  = &Error{Code: 1555}
	ErrConstraintTrigger     = &Error{Code: 1811}
	ErrConstraintUnique      = &Error{Code: 2067}
	ErrBusySnapshot          = &Error{Code: 517}
	ErrReadOnlyDatabaseMoved = &Error{Code: 1032}
)

var codeNames = map[int]string{
	1: "SQLITE_ERROR", 2: "SQLITE_INTERNAL", 3: "SQLITE_PERM", 4: "SQLITE_ABORT",
	5: "SQLITE_BUSY", 6: "SQLITE_LOCKED", 7: "SQLITE_NOMEM", 8: "SQLITE_READONLY",
	9: "SQLITE_INTERRUPT", 10: "SQLITE_IOERR", 11: "SQLITE_CORRUPT", 12: "SQLITE_NOTFOUND",
	13: "SQLITE_FULL", 14: "SQLITE_CANTOPEN", 15: "SQLITE_PROTOCOL", 16: "SQLITE_EMPTY",
	17: "SQLITE_SCHEMA", 18: "SQLITE_TOOBIG", 19: "SQLITE_CONSTRAINT", 20: "SQLITE_MISMATCH",
	21: "SQLITE_MISUSE", 22: "SQLITE_NOLFS", 23: "SQLITE_AUTH", 24: "SQLITE_FORMAT",
	25: "SQLITE_RANGE", 26: "SQLITE_NOTADB", 27: "SQLITE_NOTICE", 28: "SQLITE_WARNING",
	257: "SQLITE_ERROR_MISSING_COLLSEQ", 513: "SQLITE_ERROR_RETRY", 769: "SQLITE_ERROR_SNAPSHOT",
	261: "SQLITE_BUSY_RECOVERY", 517: "SQLITE_BUSY_SNAPSHOT", 773: "SQLITE_BUSY_TIMEOUT",
	262: "SQLITE_LOCKED_SHAREDCACHE", 518: "SQLITE_LOCKED_VTAB",
	264: "SQLITE_READONLY_RECOVERY", 520: "SQLITE_READONLY_CANTLOCK", 776: "SQLITE_READONLY_ROLLBACK",
	1032: "SQLITE_READONLY_DBMOVED", 1288: "SQLITE_READONLY_CANTINIT", 1544: "SQLITE_READONLY_DIRECTORY",
	266: "SQLITE_IOERR_READ", 522: "SQLITE_IOERR_SHORT_READ", 778: "SQLITE_IOERR_WRITE",
	1034: "SQLITE_IOERR_FSYNC", 1290: "SQLITE_IOERR_DIR_FSYNC", 1546: "SQLITE_IOERR_TRUNCATE",
	1802: "SQLITE_IOERR_FSTAT", 2058: "SQLITE_IOERR_UNLOCK", 2314: "SQLITE_IOERR_RDLOCK",
	2570: "SQLITE_IOERR_DELETE", 3082: "SQLITE_IOERR_NOMEM", 3338: "SQLITE_IOERR_ACCESS",
	3594: "SQLITE_IOERR_CHECKRESERVEDLOCK", 3850: "SQLITE_IOERR_LOCK", 4106: "SQLITE_IOERR_CLOSE",
	4618: "SQLITE_IOERR_SHMOPEN", 4874: "SQLITE_IOERR_SHMSIZE", 5130: "SQLITE_IOERR_SHMLOCK",
	5386: "SQLITE_IOERR_SHMMAP", 5642: "SQLITE_IOERR_SEEK", 5898: "SQLITE_IOERR_DELETE_NOENT",
	6154: "SQLITE_IOERR_MMAP", 6410: "SQLITE_IOERR_GETTEMPPATH", 6666: "SQLITE_IOERR_CONVPATH",
	267: "SQLITE_CORRUPT_VTAB", 523: "SQLITE_CORRUPT_SEQUENCE", 779: "SQLITE_CORRUPT_INDEX",
	270: "SQLITE_CANTOPEN_NOTEMPDIR", 526: "SQLITE_CANTOPEN_ISDIR", 782: "SQLITE_CANTOPEN_FULLPATH",
	1038: "SQLITE_CANTOPEN_CONVPATH", 1550: "SQLITE_CANTOPEN_SYMLINK",
	516: "SQLITE_ABORT_ROLLBACK",
	275: "SQLITE_CONSTRAINT_CHECK", 531: "SQLITE_CONSTRAINT_COMMITHOOK", 787: "SQLITE_CONSTRAINT_FOREIGNKEY",
	1043: "SQLITE_CONSTRAINT_FUNCTION", 1299: "SQLITE_CONSTRAINT_NOTNULL", 1555: "SQLITE_CONSTRAINT_PRIMARYKEY",
	1811: "SQLITE_CONSTRAINT_TRIGGER", 2067: "SQLITE_CONSTRAINT_UNIQUE", 2323: "SQLITE_CONSTRAINT_VTAB",
	2579: "SQLITE_CONSTRAINT_ROWID", 2835: "SQLITE_CONSTRAINT_PINNED", 3091: "SQLITE_CONSTRAINT_DATATYPE",
}

// CodeName is a result code's name, as SQLITE_CONSTRAINT_UNIQUE.
func CodeName(code int) string {
	if n, ok := codeNames[code]; ok {
		return n
	}
	if n, ok := codeNames[code&0xff]; ok {
		return n + "[" + strconv.Itoa(code) + "]"
	}
	return "SQLITE[" + strconv.Itoa(code) + "]"
}
