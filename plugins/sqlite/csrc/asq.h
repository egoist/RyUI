// asq is asuka/sqlite's side of SQLite: one entry point that Go calls
// with a whole statement's arguments and gets its answer (its rows, a
// chunk at a time) back in one crossing, on every platform: through
// purego (macOS, Linux, iOS, Android), syscall (Windows) and JavaScript
// (the web, SQLite built to WebAssembly).
//
// A call: asq_call(op, conn, arg, in, inLen, out, outCap). Its answer is
// written into out; one that doesn't fit is kept, and the call returns
// minus its length: TAKE then copies it into a buffer that big. Every
// argument is pointer-sized, so 32-bit platforms pass them in registers
// as 64-bit ones do; 64-bit values go in the buffers.
//
// An answer:
//
//	u8  ok
//	u32 the offset of its tables (from the answer's start)
//	... the op's own (ok), or u32 code, u8 autocommit, string message
//	u32 n, n strings: the tables the call's commits wrote
//	u32 n, n strings: the tables its statements read (when asked)
//
// Strings are u32 length then bytes; integers little-endian.
#ifndef ASQ_H
#define ASQ_H

#include <stddef.h>
#include <stdint.h>

#define ASQ_OPEN 1        // in: u32 busy ms, string path. out: u64 conn
#define ASQ_CLOSE 2       // conn
#define ASQ_EXEC 3        // in: u8 flags, string sql, args. out: i64 last id, i64 changes
#define ASQ_QUERY 4       // in: u8 flags, string sql, args. out: u32 cursor, columns, a chunk
#define ASQ_NEXT 5        // arg: cursor. in: u8 flags. out: a chunk
#define ASQ_CLOSE_ROWS 6  // arg: cursor. in: u8 flags
#define ASQ_TAKE 7        // copies the kept answer into out
#define ASQ_INTERRUPT 8   // conn: stops what runs on it (from any thread)
#define ASQ_SERIALIZE 9   // in: u8 flags. out: u64 n, n bytes: the main database
#define ASQ_RESTORE 10    // in: u8 flags, u64 n, n bytes: replaces the main database

// Flags: record the tables read.
#define ASQ_READS 1

// Values (arguments and columns): u8 type, then
//   0 null; 1 i64; 2 f64; 3 text: u32 n, n bytes; 4 blob: u32 n, n bytes.
// Arguments: u32 n, then each: string name ("" for none), a value.
// Columns: u32 n, then each: string name, string declared type.
// A chunk: u8 done, u32 rows, then each row's values.

#ifdef _WIN32
#define ASQ_EXPORT __declspec(dllexport)
#else
#define ASQ_EXPORT __attribute__((visibility("default")))
#endif

ASQ_EXPORT intptr_t asq_call(uintptr_t op, uintptr_t conn, uintptr_t arg, const uint8_t *in, uintptr_t inLen, uint8_t *out, uintptr_t outCap);

// asq_version is SQLite's version, as 3054001.
ASQ_EXPORT intptr_t asq_version(void);

#endif
