/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package device

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	NoisePublicKeySize    = 32
	NoisePrivateKeySize   = 32
	NoisePresharedKeySize = 32
)

type (
	NoisePublicKey    [NoisePublicKeySize]byte
	NoisePrivateKey   [NoisePrivateKeySize]byte
	NoisePresharedKey [NoisePresharedKeySize]byte
	NoiseNonce        uint64 // padded to 12-bytes
)

func loadExactHex(dst []byte, src string) error {
	slice, err := hex.DecodeString(src)
	if err != nil {
		return err
	}
	if len(slice) != len(dst) {
		return errors.New("hex string does not fit the slice")
	}
	copy(dst, slice)
	return nil
}

func (key NoisePrivateKey) IsZero() bool {
	var zero NoisePrivateKey
	return key.Equals(zero)
}

func (key NoisePrivateKey) Equals(tar NoisePrivateKey) bool {
	return subtle.ConstantTimeCompare(key[:], tar[:]) == 1
}

func (key *NoisePrivateKey) FromHex(src string) (err error) {
	err = loadExactHex(key[:], src)
	key.clamp()
	return
}

func (key *NoisePrivateKey) FromMaybeZeroHex(src string) (err error) {
	err = loadExactHex(key[:], src)
	if key.IsZero() {
		return
	}
	key.clamp()
	return
}

func (key *NoisePublicKey) FromHex(src string) error {
	return loadExactHex(key[:], src)
}

func (key NoisePublicKey) IsZero() bool {
	var zero NoisePublicKey
	return key.Equals(zero)
}

func (key NoisePublicKey) Equals(tar NoisePublicKey) bool {
	return subtle.ConstantTimeCompare(key[:], tar[:]) == 1
}

func (key *NoisePresharedKey) FromHex(src string) error {
	return loadExactHex(key[:], src)
}

const (
	HeaderCipherKeySize   = 32
	HeaderCipherNonceSize = 12
)

type HeaderCipherKey [HeaderCipherKeySize]byte

func (key HeaderCipherKey) IsZero() bool {
	var zero HeaderCipherKey
	return key.Equals(zero)
}

func (key HeaderCipherKey) Equals(tar HeaderCipherKey) bool {
	return subtle.ConstantTimeCompare(key[:], tar[:]) == 1
}

func (key *HeaderCipherKey) FromHex(src string) error {
	return loadExactHex(key[:], src)
}

type UintRange uint64

func (r *UintRange) FromUint32(lo, hi uint32) {
	*r = UintRange(uint64(hi)<<32 | uint64(lo))
}

func (r *UintRange) FromString(str string) error {
	parts := strings.Split(str, "-")
	if len(parts) < 1 || len(parts) > 2 {
		return errors.New("wrong format")
	}

	lo, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return err
	}

	hi := lo
	if len(parts) > 1 {
		hi, err = strconv.ParseUint(parts[1], 10, 32)
		if err != nil {
			return err
		}
	}

	if hi < lo {
		return errors.New("wrong range specified")
	}

	r.FromUint32(uint32(lo), uint32(hi))
	return nil
}

func (r UintRange) Contains(num uint32) bool {
	lo, hi := uint32(r), uint32(r>>32)
	return lo <= num && num <= hi
}

func (r UintRange) IsZero() bool {
	return r == 0
}

func (r UintRange) PickOne() uint32 {
	lo, hi := uint32(r), uint32(r>>32)
	if hi <= lo {
		return lo
	}
	return lo + fastrandn(hi-lo+1)
}

func (r UintRange) ToString() string {
	lo, hi := uint32(r), uint32(r>>32)
	if lo == hi {
		return fmt.Sprintf("%d", lo)
	}
	return fmt.Sprintf("%d-%d", lo, hi)
}

func (r UintRange) Lo() uint32 {
	return uint32(r)
}

func (r UintRange) Hi() uint32 {
	return uint32(r >> 32)
}

type AtomicUintRange struct {
	v atomic.Uint64
}

func (a *AtomicUintRange) Load() UintRange {
	return UintRange(a.v.Load())
}

func (a *AtomicUintRange) Store(r UintRange) {
	a.v.Store(uint64(r))
}
