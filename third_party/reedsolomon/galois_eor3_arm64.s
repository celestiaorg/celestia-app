//go:build !noasm && !appengine && !gccgo && !nopshufb

// EOR3 (FEAT_SHA3) variants of the Leopard GF(2^16) NEON kernels. Same data
// and table layout as galois_leopard_arm64.s; the four nibble lookups per
// output byte are folded with two VEOR3 instead of four VEOR. Only called
// when options.useSHA3 is set.

#include "textflag.h"

#define LOAD_TABLES3(R) \
	VLD1.P 64(R), [V0.B16, V1.B16, V2.B16, V3.B16] \
	VLD1   (R), [V4.B16, V5.B16, V6.B16, V7.B16]

#define LOAD_MASK3 \
	MOVD $0x0f, R3      \
	VMOV R3, V8.B[0]    \
	VDUP V8.B[0], V8.B16

// (XLO, XHI) ^= (LO, HI) * log_m for 16 symbols. Scratch V9-V15.
#define MULXOR16(LO, HI, XLO, XHI) \
	VAND  V8.B16, LO.B16, V9.B16   \
	VUSHR $4, LO.B16, V10.B16      \
	VAND  V8.B16, HI.B16, V11.B16  \
	VUSHR $4, HI.B16, V12.B16      \
	VTBL  V9.B16, [V0.B16], V13.B16  \
	VTBL  V10.B16, [V1.B16], V14.B16 \
	VTBL  V11.B16, [V2.B16], V15.B16 \
	VEOR3 V13.B16, V14.B16, XLO.B16, XLO.B16 \
	VTBL  V12.B16, [V3.B16], V13.B16 \
	VEOR3 V15.B16, V13.B16, XLO.B16, XLO.B16 \
	VTBL  V9.B16, [V4.B16], V13.B16  \
	VTBL  V10.B16, [V5.B16], V14.B16 \
	VTBL  V11.B16, [V6.B16], V15.B16 \
	VEOR3 V13.B16, V14.B16, XHI.B16, XHI.B16 \
	VTBL  V12.B16, [V7.B16], V13.B16 \
	VEOR3 V15.B16, V13.B16, XHI.B16, XHI.B16

// Row A (lo0, lo1, hi0, hi1) ^= row B * log_m; both are 4-register rows.
#define MULXOR_ROW(B0, B1, B2, B3, A0, A1, A2, A3) \
	MULXOR16(B0, B2, A0, A2) \
	MULXOR16(B1, B3, A1, A3)

// Row B ^= row A.
#define XOR_ROW(A0, A1, A2, A3, B0, B1, B2, B3) \
	VEOR A0.B16, B0.B16, B0.B16 \
	VEOR A1.B16, B1.B16, B1.B16 \
	VEOR A2.B16, B2.B16, B2.B16 \
	VEOR A3.B16, B3.B16, B3.B16

// x (V20-V23) ^= y (V16-V19) * log_m.
#define MULXOR64 \
	MULXOR16(V16, V18, V20, V22) \
	MULXOR16(V17, V19, V21, V23)

// func ifftDIT2NEON3(x, y []byte, table *[128]uint8)
// y ^= x; x ^= y * log_m
TEXT ·ifftDIT2NEON3(SB), NOSPLIT, $0-56
	MOVD table+48(FP), R10
	LOAD_TABLES3(R10)
	LOAD_MASK3
	MOVD x_base+0(FP), R1
	MOVD x_len+8(FP), R2
	MOVD y_base+24(FP), R5
	CBZ  R2, ifft2e3done

ifft2e3loop:
	VLD1 (R1), [V20.B16, V21.B16, V22.B16, V23.B16]
	VLD1 (R5), [V16.B16, V17.B16, V18.B16, V19.B16]
	VEOR V20.B16, V16.B16, V16.B16
	VEOR V21.B16, V17.B16, V17.B16
	VEOR V22.B16, V18.B16, V18.B16
	VEOR V23.B16, V19.B16, V19.B16
	VST1.P [V16.B16, V17.B16, V18.B16, V19.B16], 64(R5)
	MULXOR64
	VST1.P [V20.B16, V21.B16, V22.B16, V23.B16], 64(R1)
	SUBS $64, R2
	BGT  ifft2e3loop

ifft2e3done:
	RET

// func fftDIT2NEON3(x, y []byte, table *[128]uint8)
// x ^= y * log_m; y ^= x
TEXT ·fftDIT2NEON3(SB), NOSPLIT, $0-56
	MOVD table+48(FP), R10
	LOAD_TABLES3(R10)
	LOAD_MASK3
	MOVD x_base+0(FP), R1
	MOVD x_len+8(FP), R2
	MOVD y_base+24(FP), R5
	CBZ  R2, fft2e3done

fft2e3loop:
	VLD1 (R5), [V16.B16, V17.B16, V18.B16, V19.B16]
	VLD1 (R1), [V20.B16, V21.B16, V22.B16, V23.B16]
	MULXOR64
	VST1.P [V20.B16, V21.B16, V22.B16, V23.B16], 64(R1)
	VEOR V20.B16, V16.B16, V16.B16
	VEOR V21.B16, V17.B16, V17.B16
	VEOR V22.B16, V18.B16, V18.B16
	VEOR V23.B16, V19.B16, V19.B16
	VST1.P [V16.B16, V17.B16, V18.B16, V19.B16], 64(R5)
	SUBS $64, R2
	BGT  fft2e3loop

fft2e3done:
	RET

// func mulgf16XorNEON3(x, y []byte, table *[128]uint8)
// x ^= y * log_m
TEXT ·mulgf16XorNEON3(SB), NOSPLIT, $0-56
	MOVD table+48(FP), R10
	LOAD_TABLES3(R10)
	LOAD_MASK3
	MOVD x_base+0(FP), R1
	MOVD x_len+8(FP), R2
	MOVD y_base+24(FP), R5
	CBZ  R2, mulxore3done

mulxore3loop:
	VLD1.P 64(R5), [V16.B16, V17.B16, V18.B16, V19.B16]
	VLD1 (R1), [V20.B16, V21.B16, V22.B16, V23.B16]
	MULXOR64
	VST1.P [V20.B16, V21.B16, V22.B16, V23.B16], 64(R1)
	SUBS $64, R2
	BGT  mulxore3loop

mulxore3done:
	RET

// func splitMulXorNEON3(x, y []byte, table *[128]uint8)
// y = x; x ^= x * log_m
TEXT ·splitMulXorNEON3(SB), NOSPLIT, $0-56
	MOVD table+48(FP), R10
	LOAD_TABLES3(R10)
	LOAD_MASK3
	MOVD x_base+0(FP), R1
	MOVD x_len+8(FP), R2
	MOVD y_base+24(FP), R5
	CBZ  R2, splite3done

splite3loop:
	VLD1 (R1), [V16.B16, V17.B16, V18.B16, V19.B16]
	VST1.P [V16.B16, V17.B16, V18.B16, V19.B16], 64(R5)
	VORR V16.B16, V16.B16, V20.B16
	VORR V17.B16, V17.B16, V21.B16
	VORR V18.B16, V18.B16, V22.B16
	VORR V19.B16, V19.B16, V23.B16
	MULXOR64
	VST1.P [V20.B16, V21.B16, V22.B16, V23.B16], 64(R1)
	SUBS $64, R2
	BGT  splite3loop

splite3done:
	RET

// Fused radix-4 butterflies. Rows 0-3 live in V16-V19, V20-V23, V24-V27,
// V28-V31 for a whole 64-byte block; the tables for each multiplier are
// reloaded into V0-V7 from L1. Callers exclude log_m == modulus.

// func fftDIT4NEON3(w0, w1, w2, w3 []byte, t01, t23, t02 *[128]uint8)
TEXT ·fftDIT4NEON3(SB), NOSPLIT, $0-120
	MOVD w0_base+0(FP), R1
	MOVD w0_len+8(FP), R2
	MOVD w1_base+24(FP), R4
	MOVD w2_base+48(FP), R5
	MOVD w3_base+72(FP), R6
	MOVD t01+96(FP), R7
	MOVD t23+104(FP), R8
	MOVD t02+112(FP), R9
	LOAD_MASK3
	CBZ  R2, fft4done

fft4loop:
	VLD1 (R1), [V16.B16, V17.B16, V18.B16, V19.B16]
	VLD1 (R4), [V20.B16, V21.B16, V22.B16, V23.B16]
	VLD1 (R5), [V24.B16, V25.B16, V26.B16, V27.B16]
	VLD1 (R6), [V28.B16, V29.B16, V30.B16, V31.B16]
	// Layer 1: (w0, w2) and (w1, w3) with log_m02.
	MOVD R9, R10
	LOAD_TABLES3(R10)
	MULXOR_ROW(V24, V25, V26, V27, V16, V17, V18, V19)
	XOR_ROW(V16, V17, V18, V19, V24, V25, V26, V27)
	MULXOR_ROW(V28, V29, V30, V31, V20, V21, V22, V23)
	XOR_ROW(V20, V21, V22, V23, V28, V29, V30, V31)
	// Layer 2: (w0, w1) with log_m01, (w2, w3) with log_m23.
	MOVD R7, R10
	LOAD_TABLES3(R10)
	MULXOR_ROW(V20, V21, V22, V23, V16, V17, V18, V19)
	XOR_ROW(V16, V17, V18, V19, V20, V21, V22, V23)
	MOVD R8, R10
	LOAD_TABLES3(R10)
	MULXOR_ROW(V28, V29, V30, V31, V24, V25, V26, V27)
	XOR_ROW(V24, V25, V26, V27, V28, V29, V30, V31)
	VST1.P [V16.B16, V17.B16, V18.B16, V19.B16], 64(R1)
	VST1.P [V20.B16, V21.B16, V22.B16, V23.B16], 64(R4)
	VST1.P [V24.B16, V25.B16, V26.B16, V27.B16], 64(R5)
	VST1.P [V28.B16, V29.B16, V30.B16, V31.B16], 64(R6)
	SUBS $64, R2
	BGT  fft4loop

fft4done:
	RET

// func ifftDIT4NEON3(w0, w1, w2, w3 []byte, t01, t23, t02 *[128]uint8)
TEXT ·ifftDIT4NEON3(SB), NOSPLIT, $0-120
	MOVD w0_base+0(FP), R1
	MOVD w0_len+8(FP), R2
	MOVD w1_base+24(FP), R4
	MOVD w2_base+48(FP), R5
	MOVD w3_base+72(FP), R6
	MOVD t01+96(FP), R7
	MOVD t23+104(FP), R8
	MOVD t02+112(FP), R9
	LOAD_MASK3
	CBZ  R2, ifft4done

ifft4loop:
	VLD1 (R1), [V16.B16, V17.B16, V18.B16, V19.B16]
	VLD1 (R4), [V20.B16, V21.B16, V22.B16, V23.B16]
	VLD1 (R5), [V24.B16, V25.B16, V26.B16, V27.B16]
	VLD1 (R6), [V28.B16, V29.B16, V30.B16, V31.B16]
	// Layer 1: (w0, w1) with log_m01, (w2, w3) with log_m23.
	MOVD R7, R10
	LOAD_TABLES3(R10)
	XOR_ROW(V16, V17, V18, V19, V20, V21, V22, V23)
	MULXOR_ROW(V20, V21, V22, V23, V16, V17, V18, V19)
	MOVD R8, R10
	LOAD_TABLES3(R10)
	XOR_ROW(V24, V25, V26, V27, V28, V29, V30, V31)
	MULXOR_ROW(V28, V29, V30, V31, V24, V25, V26, V27)
	// Layer 2: (w0, w2) and (w1, w3) with log_m02.
	MOVD R9, R10
	LOAD_TABLES3(R10)
	XOR_ROW(V16, V17, V18, V19, V24, V25, V26, V27)
	MULXOR_ROW(V24, V25, V26, V27, V16, V17, V18, V19)
	XOR_ROW(V20, V21, V22, V23, V28, V29, V30, V31)
	MULXOR_ROW(V28, V29, V30, V31, V20, V21, V22, V23)
	VST1.P [V16.B16, V17.B16, V18.B16, V19.B16], 64(R1)
	VST1.P [V20.B16, V21.B16, V22.B16, V23.B16], 64(R4)
	VST1.P [V24.B16, V25.B16, V26.B16, V27.B16], 64(R5)
	VST1.P [V28.B16, V29.B16, V30.B16, V31.B16], 64(R6)
	SUBS $64, R2
	BGT  ifft4loop

ifft4done:
	RET
