//go:build windows

#include "textflag.h"

// fcallTramp is called as a C function (by asmstdcall, through
// syscall.SyscallN) with an *fframe in R0: it loads the frame's
// arguments into X0-X7 and D0-D7 and jumps to its function, which
// returns to asmstdcall (LR is untouched).
TEXT fcallTramp<>(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R0, R9
	FMOVD	72(R9), F0
	FMOVD	80(R9), F1
	FMOVD	88(R9), F2
	FMOVD	96(R9), F3
	FMOVD	104(R9), F4
	FMOVD	112(R9), F5
	FMOVD	120(R9), F6
	FMOVD	128(R9), F7
	MOVD	8(R9), R0
	MOVD	16(R9), R1
	MOVD	24(R9), R2
	MOVD	32(R9), R3
	MOVD	40(R9), R4
	MOVD	48(R9), R5
	MOVD	56(R9), R6
	MOVD	64(R9), R7
	MOVD	0(R9), R10
	JMP	(R10)

// func fcallTrampPC() uintptr
TEXT ·fcallTrampPC(SB), NOSPLIT, $0-8
	MOVD	$fcallTramp<>(SB), R0
	MOVD	R0, ret+0(FP)
	RET
