// Copyright 2015, Joe Tsai. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package gzipbuilder

import (
	"hash/crc32"
	"testing"
)

func TestCombineCRC32(t *testing.T) {
	var golden = []struct {
		ieee, castagnoli, koopman uint32
		in                        string
	}{
		{0x00000000, 0x00000000, 0x00000000, ""},
		{0xe8b7be43, 0xc1d04330, 0x0da2aa8a, "a"},
		{0x9e83486d, 0xe2a22936, 0x31ec935a, "ab"},
		{0x352441c2, 0x364b3fb7, 0xba2322ac, "abc"},
		{0xed82cd11, 0x92c80a31, 0xe0a6bcf7, "abcd"},
		{0x8587d865, 0xc450d697, 0xac046415, "abcde"},
		{0x4b8e39ef, 0x53bceff1, 0x7589981b, "abcdef"},
		{0x312a6aa6, 0xe627f441, 0x7999acb5, "abcdefg"},
		{0xaeef2a50, 0x0a9421b7, 0xd5cc0e40, "abcdefgh"},
		{0x8da988af, 0x2ddc99fc, 0x39080d0d, "abcdefghi"},
		{0x3981703a, 0xe6599437, 0xd6205881, "abcdefghij"},
		{0x6b9cdfe7, 0xb2cc01fe, 0x418f6bac, "Discard medicine more than two years old."},
		{0xc90ef73f, 0x0e28207f, 0x847e1e04, "He who has a shady past knows that nice guys finish last."},
		{0xb902341f, 0xbe93f964, 0x606bf5a6, "I wouldn't marry him with a ten foot pole."},
		{0x042080e8, 0x9e3be0c3, 0x1521d7b7, "Free! Free!/A trip/to Mars/for 900/empty jars/Burma Shave"},
		{0x154c6d11, 0xf505ef04, 0xe238d024, "The days of the digital watch are numbered.  -Tom Stoppard"},
		{0x4c418325, 0x85d3dc82, 0x5423e28a, "Nepal premier won't resign."},
		{0x33955150, 0xc5142380, 0x97f7c3a6, "For every action there is an equal and opposite government program."},
		{0x26216a4b, 0x75eb77dd, 0xe4543ac6, "His money is twice tainted: 'taint yours and 'taint mine."},
		{0x1abbe45e, 0x91ebe9f7, 0x48ec4d9a, "There is no reason for any individual to have a computer in their home. -Ken Olsen, 1977"},
		{0xc89a94f7, 0xf0b1168e, 0xc75afda4, "It's a tiny change to the code and not completely disgusting. - Bob Manchek"},
		{0xab3abe14, 0x572b74e2, 0x6db40154, "size:  a.out:  bad magic"},
		{0xbab102b6, 0x8a58a6d5, 0x4c148ba0, "The major problem is with sendmail.  -Mark Horton"},
		{0x999149d7, 0x9c426c50, 0x9be6c237, "Give me a rock, paper and scissors and I will move the world.  CCFestoon"},
		{0x6d52a33c, 0x735400a4, 0x52f8abfc, "If the enemy is within range, then so are you."},
		{0x90631e8d, 0xbec49c95, 0xf98e0b1d, "It's well we cannot hear the screams/That we create in others' dreams."},
		{0x78309130, 0xa95a2079, 0x6a1d5514, "You remind me of a TV show, but that's all right: I watch it anyway."},
		{0x7d0a377f, 0xde2e65c5, 0xd88bc947, "C is as portable as Stonehedge!!"},
		{0x8c79fd79, 0x297a88ed, 0x5e625378, "Even if I could be Shakespeare, I think I should still choose to be Faraday. - A. Huxley"},
		{0xa20b7167, 0x66ed1d8b, 0xbd1004ed, "The fugacity of a constituent in a mixture of gases at a given temperature is proportional to its mole fraction.  Lewis-Randall Rule"},
		{0x8e0bb443, 0xdcded527, 0xd4575591, "How can you write a big system without C++?  -Paul Glick"},
	}

	var (
		CastagnoliTable = crc32.MakeTable(crc32.Castagnoli)
		KoopmanTable    = crc32.MakeTable(crc32.Koopman)
	)

	var ChecksumIEEE = func(data []byte) uint32 {
		return crc32.ChecksumIEEE(data)
	}
	var ChecksumCastagnoli = func(data []byte) uint32 {
		return crc32.Checksum(data, CastagnoliTable)
	}
	var ChecksumKoopman = func(data []byte) uint32 {
		return crc32.Checksum(data, KoopmanTable)
	}

	var (
		IEEMat        = precomputeCRC32(crc32.IEEE)
		CastagnoliMat = precomputeCRC32(crc32.Castagnoli)
		KoopmanMat    = precomputeCRC32(crc32.Koopman)
	)

	for _, g := range golden {
		var splits = []int{
			0 * (len(g.in) / 1),
			1 * (len(g.in) / 4),
			2 * (len(g.in) / 4),
			3 * (len(g.in) / 4),
			1 * (len(g.in) / 1),
		}

		for _, i := range splits {
			p1, p2 := []byte(g.in[:i]), []byte(g.in[i:])
			in1, in2 := g.in[:i], g.in[i:]
			len2 := uint64(len(p2))
			if got := combineCRC32(IEEMat, ChecksumIEEE(p1), ChecksumIEEE(p2), len2); got != g.ieee {
				t.Errorf("combineCRC32(precomputeCRC32(IEEE), ChecksumIEEE(%q), ChecksumIEEE(%q), %d) = 0x%x, want 0x%x",
					in1, in2, len2, got, g.ieee)
			}
			if got := combineCRC32(CastagnoliMat, ChecksumCastagnoli(p1), ChecksumCastagnoli(p2), len2); got != g.castagnoli {
				t.Errorf("combineCRC32(precomputeCRC32(Castagnoli), ChecksumCastagnoli(%q), ChecksumCastagnoli(%q), %d) = 0x%x, want 0x%x",
					in1, in2, len2, got, g.castagnoli)
			}
			if got := combineCRC32(KoopmanMat, ChecksumKoopman(p1), ChecksumKoopman(p2), len2); got != g.koopman {
				t.Errorf("combineCRC32(precomputeCRC32(Koopman), ChecksumKoopman(%q), ChecksumKoopman(%q), %d) = 0x%x, want 0x%x",
					in1, in2, len2, got, g.koopman)
			}
		}
	}
}

func TestCombineCRC32Long(t *testing.T) {
	mat := precomputeCRC32(crc32.IEEE)

	// This is a regression test for long values of len2.
	for _, tc := range []struct {
		len2   uint64
		expect uint32
	}{
		{1 << 7, 0x6d331acc},
		{1 << 15, 0x4c8ded7f},
		{1 << 31, 0xa360d9f3},
		{1 << 39, 0x6d331acc},
		{1 << 47, 0x4c8ded7f},
	} {
		if got := combineCRC32(mat, 0xdeadbeef, 0x1337f001, tc.len2); got != tc.expect {
			t.Errorf("combineCRC32(precomputeCRC32(Koopman), 0xdeadbeef, 0x1337f001, %d) = 0x%x, want 0x%x",
				tc.len2, got, tc.expect)
		}
	}
}

func BenchmarkPrecomputeCRC32(b *testing.B) {
	var mat *crc32Matrix

	for n := 0; n < b.N; n++ {
		mat = precomputeCRC32(crc32.IEEE)
	}

	_ = mat
}
