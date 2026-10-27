//go:build arm64 && !noasm && !race

#include "textflag.h"

// Two copies of crypto/sha256's arm64 block function (sha256block_arm64.s),
// interleaved so the latency-bound SHA256H chains of the two lanes overlap.
//
// Lane a: state V0/V1, working V2/V3/V8, schedule V4-V7, K+W V9.
// Lane b: state V10/V11, working V12/V13/V14, schedule V16-V19, K+W V15.
// V20 holds the round constants for the current four rounds.

// KW loads the next four round constants and adds them to schedule words wa
// (lane a) and wb (lane b).
#define KW(wa, wb) \
	VLD1.P 16(R2), [V20.S4]      \
	VADD   V20.S4, wa.S4, V9.S4  \
	VADD   V20.S4, wb.S4, V15.S4

// SU0 and SU1 advance the message schedule in both lanes: SU0 updates a0 with
// a1, SU1 updates a0 with a1 and a2 (and likewise for lane b).
#define SU0(a0, a1, b0, b1) \
	SHA256SU0 a1.S4, a0.S4 \
	SHA256SU0 b1.S4, b0.S4

#define SU1(a0, a1, a2, b0, b1, b2) \
	SHA256SU1 a2.S4, a1.S4, a0.S4 \
	SHA256SU1 b2.S4, b1.S4, b0.S4

// HASH runs four rounds in both lanes. The lanes are independent, so the
// second lane's instructions fill the first lane's latency.
#define HASH \
	SHA256H  V9.S4, V3, V2    \
	SHA256H2 V9.S4, V8, V3    \
	VMOV     V2.B16, V8.B16   \
	SHA256H  V15.S4, V13, V12 \
	SHA256H2 V15.S4, V14, V13 \
	VMOV     V12.B16, V14.B16

// func block(a, b *[8]uint32, pa, pb []byte)
TEXT ·block(SB), NOSPLIT, $0-64
	MOVD a+0(FP), R0
	MOVD b+8(FP), R4
	MOVD pa_base+16(FP), R1
	MOVD pa_len+24(FP), R3
	MOVD pb_base+40(FP), R5
	CBZ  R3, done
	VLD1 (R0), [V0.S4, V1.S4]
	VLD1 (R4), [V10.S4, V11.S4]

loop:
	MOVD   $·k+0(SB), R2
	VLD1.P 64(R1), [V4.B16, V5.B16, V6.B16, V7.B16]
	VLD1.P 64(R5), [V16.B16, V17.B16, V18.B16, V19.B16]
	VMOV   V0.B16, V2.B16
	VMOV   V1.B16, V3.B16
	VMOV   V0.B16, V8.B16
	VMOV   V10.B16, V12.B16
	VMOV   V11.B16, V13.B16
	VMOV   V10.B16, V14.B16
	VREV32 V4.B16, V4.B16
	VREV32 V5.B16, V5.B16
	VREV32 V6.B16, V6.B16
	VREV32 V7.B16, V7.B16
	VREV32 V16.B16, V16.B16
	VREV32 V17.B16, V17.B16
	VREV32 V18.B16, V18.B16
	VREV32 V19.B16, V19.B16

	// rounds 0-47: schedule the next words while hashing
	KW(V4, V16); SU0(V4, V5, V16, V17); HASH
	KW(V5, V17); SU0(V5, V6, V17, V18); SU1(V4, V6, V7, V16, V18, V19); HASH
	KW(V6, V18); SU0(V6, V7, V18, V19); SU1(V5, V7, V4, V17, V19, V16); HASH
	KW(V7, V19); SU0(V7, V4, V19, V16); SU1(V6, V4, V5, V18, V16, V17); HASH
	KW(V4, V16); SU0(V4, V5, V16, V17); SU1(V7, V5, V6, V19, V17, V18); HASH
	KW(V5, V17); SU0(V5, V6, V17, V18); SU1(V4, V6, V7, V16, V18, V19); HASH
	KW(V6, V18); SU0(V6, V7, V18, V19); SU1(V5, V7, V4, V17, V19, V16); HASH
	KW(V7, V19); SU0(V7, V4, V19, V16); SU1(V6, V4, V5, V18, V16, V17); HASH
	KW(V4, V16); SU0(V4, V5, V16, V17); SU1(V7, V5, V6, V19, V17, V18); HASH
	KW(V5, V17); SU0(V5, V6, V17, V18); SU1(V4, V6, V7, V16, V18, V19); HASH
	KW(V6, V18); SU0(V6, V7, V18, V19); SU1(V5, V7, V4, V17, V19, V16); HASH
	KW(V7, V19); SU0(V7, V4, V19, V16); SU1(V6, V4, V5, V18, V16, V17); HASH

	// rounds 48-63: the last schedule update, then hash the remaining words
	KW(V4, V16); SU1(V7, V5, V6, V19, V17, V18); HASH
	KW(V5, V17); HASH
	KW(V6, V18); HASH
	KW(V7, V19); HASH

	VADD V2.S4, V0.S4, V0.S4
	VADD V3.S4, V1.S4, V1.S4
	VADD V12.S4, V10.S4, V10.S4
	VADD V13.S4, V11.S4, V11.S4
	SUBS $64, R3, R3
	BNE  loop
	VST1 [V0.S4, V1.S4], (R0)
	VST1 [V10.S4, V11.S4], (R4)

done:
	RET
