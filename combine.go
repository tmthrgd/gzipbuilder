// Copyright 2015, Joe Tsai. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package gzipbuilder

import "math/bits"

// The origin of the CombineAdler32, CombineCRC32, and CombineCRC64 functions
// in this package is the adler32_combine, crc32_combine, gf2_matrix_times,
// and gf2_matrix_square functions found in the zlib library and was translated
// from C to Go. Thanks goes to the authors of zlib:
//	Mark Adler and Jean-loup Gailly.
//
// See the following:
//	https://www.zlib.net/
//	https://github.com/madler/zlib/blob/master/adler32.c
//	https://github.com/madler/zlib/blob/master/crc32.c
//	https://stackoverflow.com/questions/23122312/crc-calculation-of-a-mostly-static-data-stream/23126768#23126768
//
// ====================================================
// Copyright (C) 1995-2013 Jean-loup Gailly and Mark Adler
//
// This software is provided 'as-is', without any express or implied
// warranty.  In no event will the authors be held liable for any damages
// arising from the use of this software.
//
// Permission is granted to anyone to use this software for any purpose,
// including commercial applications, and to alter it and redistribute it
// freely, subject to the following restrictions:
//
// 1. The origin of this software must not be misrepresented; you must not
//    claim that you wrote the original software. If you use this software
//    in a product, an acknowledgment in the product documentation would be
//    appreciated but is not required.
// 2. Altered source versions must be plainly marked as such, and must not be
//    misrepresented as being the original software.
// 3. This notice may not be removed or altered from any source distribution.
//
// Jean-loup Gailly        Mark Adler
// jloup@gzip.org          madler@alumni.caltech.edu
// ====================================================

// Translation of gf2_matrix_times from zlib.
func matrixMult(mat *[32]uint32, vec uint32) uint32 {
	var sum uint32

	for n := 0; n < len(mat); n, vec = n+4, vec>>4 {
		if vec&(1<<0) != 0 {
			sum ^= mat[n+0]
		}
		if vec&(1<<1) != 0 {
			sum ^= mat[n+1]
		}
		if vec&(1<<2) != 0 {
			sum ^= mat[n+2]
		}
		if vec&(1<<3) != 0 {
			sum ^= mat[n+3]
		}
	}

	return sum
}

// Translation of gf2_matrix_square from zlib.
func matrixSquare(square, mat *[32]uint32) {
	for n := 0; n < 32; n++ {
		square[n] = matrixMult(mat, mat[n])
	}
}

type crc32Matrix [48][32]uint32

func precomputeCRC32(poly uint32) *crc32Matrix {
	// Even and odd power-of-two zeros operators.
	var even, odd [32]uint32

	// Put operator for one zero bit in odd.
	var row uint32 = 1
	odd[0] = poly
	for n := 1; n < 32; n++ {
		odd[n] = row
		row <<= 1
	}

	// Put operator for two zero bits in even.
	matrixSquare(&even, &odd)

	// Put operator for four zero bits in odd.
	matrixSquare(&odd, &even)

	mat := new(crc32Matrix)

	for i := 0; i < len(mat); i += 2 {
		matrixSquare(&even, &odd)
		mat[i+0] = even

		matrixSquare(&odd, &even)
		mat[i+1] = odd
	}

	return mat
}

// combineCRC32 combines two CRC-32 checksums together.
// Let AB be the string concatenation of two strings A and B. Then Combine
// computes the checksum of AB given only the checksum of A, the checksum of B,
// and the length of B:
//	tab := crc32.MakeTable(poly)
//	crc32.Checksum(AB, tab) == combineCRC32(precomputeCRC32(poly), crc32.Checksum(A, tab), crc32.Checksum(B, tab), len(B))
func combineCRC32(mat *crc32Matrix, crc1, crc2 uint32, len2 uint64) uint32 {
	if len2>>48 != 0 {
		panic("gziptemplate: length out of range")
	}

	// Apply len2 zeros to crc1.
	for n := 0; len2 != 0; {
		nz := bits.TrailingZeros64(len2)
		n += nz + 1
		crc1 = matrixMult(&mat[n-1], crc1)
		len2 >>= uint(nz) + 1
	}

	return crc1 ^ crc2
}
