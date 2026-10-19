package model

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
)

// TorrentFileInfoHash hashes the original bencoded info dictionary without
// re-encoding it, preserving private-tracker metainfo exactly as received.
func TorrentFileInfoHash(data []byte) (string, error) {
	if len(data) < 2 || data[0] != 'd' {
		return "", fmt.Errorf("torrent metainfo must be a dictionary")
	}
	p := bencodeReader{data: data, pos: 1}
	var info []byte
	for p.pos < len(data) && data[p.pos] != 'e' {
		key, err := p.readString()
		if err != nil {
			return "", err
		}
		start := p.pos
		if err := p.skip(0); err != nil {
			return "", err
		}
		if string(key) == "info" {
			if info != nil || data[start] != 'd' {
				return "", fmt.Errorf("torrent requires one info dictionary")
			}
			info = data[start:p.pos]
		}
	}
	if p.pos != len(data)-1 || data[p.pos] != 'e' || info == nil {
		return "", fmt.Errorf("invalid torrent metainfo or missing info dictionary")
	}
	sum := sha1.Sum(info)
	return hex.EncodeToString(sum[:]), nil
}

type bencodeReader struct {
	data []byte
	pos  int
}

func (p *bencodeReader) readString() ([]byte, error) {
	start := p.pos
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start || p.pos >= len(p.data) || p.data[p.pos] != ':' {
		return nil, fmt.Errorf("invalid bencoded string")
	}
	size, err := strconv.Atoi(string(p.data[start:p.pos]))
	p.pos++
	if err != nil || size < 0 || size > len(p.data)-p.pos {
		return nil, fmt.Errorf("truncated bencoded string")
	}
	value := p.data[p.pos : p.pos+size]
	p.pos += size
	return value, nil
}
func (p *bencodeReader) skip(depth int) error {
	if depth > 100 || p.pos >= len(p.data) {
		return fmt.Errorf("invalid bencoded value")
	}
	switch p.data[p.pos] {
	case 'd', 'l':
		dictionary := p.data[p.pos] == 'd'
		p.pos++
		for p.pos < len(p.data) && p.data[p.pos] != 'e' {
			if dictionary {
				if _, err := p.readString(); err != nil {
					return err
				}
			}
			if err := p.skip(depth + 1); err != nil {
				return err
			}
		}
		if p.pos >= len(p.data) {
			return fmt.Errorf("unterminated bencoded container")
		}
		p.pos++
	case 'i':
		p.pos++
		start := p.pos
		if p.pos < len(p.data) && p.data[p.pos] == '-' {
			p.pos++
		}
		digits := p.pos
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == digits || p.pos >= len(p.data) || p.data[p.pos] != 'e' || p.pos == start {
			return fmt.Errorf("invalid bencoded integer")
		}
		p.pos++
	default:
		if _, err := p.readString(); err != nil {
			return err
		}
	}
	return nil
}
