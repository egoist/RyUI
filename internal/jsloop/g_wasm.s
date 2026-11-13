//go:build wasm

#include "textflag.h"

// func current() uintptr
TEXT ·current(SB), NOSPLIT, $0-8
	MOVD g, ret+0(FP)
	RET
