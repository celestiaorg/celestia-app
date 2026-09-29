//go:build arm64 && !noasm && !nopshufb && !race

#include "textflag.h"

// (OUTLO, OUTHI) = linear map of (LO, HI) for 16 symbols using the nibble
// tables in V0-V7 and mask V8; T1-T3 are scratch.
#define MAP16(LO, HI, OUTLO, OUTHI, T1, T2, T3) \
	VAND  V8.B16, LO.B16, T1.B16        \
	VUSHR $4, LO.B16, T2.B16            \
	VTBL  T1.B16, [V0.B16], OUTLO.B16   \
	VTBL  T1.B16, [V4.B16], OUTHI.B16   \
	VTBL  T2.B16, [V1.B16], T3.B16      \
	VEOR  T3.B16, OUTLO.B16, OUTLO.B16  \
	VTBL  T2.B16, [V5.B16], T3.B16      \
	VEOR  T3.B16, OUTHI.B16, OUTHI.B16  \
	VAND  V8.B16, HI.B16, T1.B16        \
	VUSHR $4, HI.B16, T2.B16            \
	VTBL  T1.B16, [V2.B16], T3.B16      \
	VEOR  T3.B16, OUTLO.B16, OUTLO.B16  \
	VTBL  T1.B16, [V6.B16], T3.B16      \
	VEOR  T3.B16, OUTHI.B16, OUTHI.B16  \
	VTBL  T2.B16, [V3.B16], T3.B16      \
	VEOR  T3.B16, OUTLO.B16, OUTLO.B16  \
	VTBL  T2.B16, [V7.B16], T3.B16      \
	VEOR  T3.B16, OUTHI.B16, OUTHI.B16

// func polyConvertNEON(dst, src []byte, tables *[128]byte)
TEXT ·polyConvertNEON(SB), NOSPLIT, $0-56
	MOVD tables+48(FP), R10
	VLD1.P 64(R10), [V0.B16, V1.B16, V2.B16, V3.B16]
	VLD1   (R10), [V4.B16, V5.B16, V6.B16, V7.B16]
	MOVD   $0x0f, R3
	VMOV   R3, V8.B[0]
	VDUP   V8.B[0], V8.B16
	MOVD   dst_base+0(FP), R1
	MOVD   src_base+24(FP), R5
	MOVD   src_len+32(FP), R2
	CBZ    R2, convdone

convloop:
	VLD1.P 64(R5), [V16.B16, V17.B16, V18.B16, V19.B16]
	MAP16(V16, V18, V24, V25, V30, V31, V9)
	MAP16(V17, V19, V27, V28, V30, V31, V9)
	VEOR   V24.B16, V25.B16, V26.B16
	VEOR   V27.B16, V28.B16, V29.B16
	VST1.P [V24.B16, V25.B16, V26.B16], 48(R1)
	VST1.P [V27.B16, V28.B16, V29.B16], 48(R1)
	SUBS   $64, R2
	BGT    convloop

convdone:
	RET

// ACC ^= both 8-lane halves of the carry-less product of B and A.
#define PROD(B, A, ACC) \
	VPMULL  B.B8, A.B8, V3.H8          \
	VPMULL2 B.B16, A.B16, V4.H8        \
	VEOR3   V3.B16, V4.B16, ACC.B16, ACC.B16

// func polyAccum4NEON(acc *[12][16]byte, a, b []byte)
TEXT ·polyAccum4NEON(SB), NOSPLIT, $0-56
	MOVD   acc+0(FP), R0
	MOVD   a_base+8(FP), R1
	MOVD   a_len+16(FP), R2
	MOVD   b_base+32(FP), R3
	MOVD   R0, R4
	VLD1.P 64(R4), [V8.B16, V9.B16, V10.B16, V11.B16]
	VLD1.P 64(R4), [V12.B16, V13.B16, V14.B16, V15.B16]
	VLD1   (R4), [V16.B16, V17.B16, V18.B16, V19.B16]
	CBZ    R2, accstore

accloop:
	VLD1.P 48(R1), [V0.B16, V1.B16, V2.B16]
	VLD1.P 64(R3), [V20.B16, V21.B16, V22.B16, V23.B16]
	VLD1.P 64(R3), [V24.B16, V25.B16, V26.B16, V27.B16]
	VLD1.P 64(R3), [V28.B16, V29.B16, V30.B16, V31.B16]
	PROD(V20, V0, V8)
	PROD(V21, V1, V9)
	PROD(V22, V2, V10)
	PROD(V23, V0, V11)
	PROD(V24, V1, V12)
	PROD(V25, V2, V13)
	PROD(V26, V0, V14)
	PROD(V27, V1, V15)
	PROD(V28, V2, V16)
	PROD(V29, V0, V17)
	PROD(V30, V1, V18)
	PROD(V31, V2, V19)
	SUBS   $48, R2
	BGT    accloop

accstore:
	VST1.P [V8.B16, V9.B16, V10.B16, V11.B16], 64(R0)
	VST1.P [V12.B16, V13.B16, V14.B16, V15.B16], 64(R0)
	VST1   [V16.B16, V17.B16, V18.B16, V19.B16], (R0)
	RET
