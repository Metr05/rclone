/*
Translate file names for usage on restrictive storage systems

The restricted set of characters are mapped to a unicode equivalent version
(most to their FULLWIDTH variant) to increase compatability with other
storage systems.
See: http://unicode-search.net/unicode-namesearch.pl?term=FULLWIDTH

Encoders will also quote reserved characters to differentiate between
the raw and encoded forms.
*/

package encoder

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// adding this to any printable ASCII character turns it into the
	// FULLWIDTH variant
	fullOffset = 0xFEE0
	// the first rune of the SYMBOL FOR block for control characters
	symbolOffset = '␀' // SYMBOL FOR NULL
	// QuoteRune is the rune used for quoting reserved characters
	QuoteRune = '‛' // SINGLE HIGH-REVERSED-9 QUOTATION MARK
	// EncodeStandard contains the flags used for the Standard Encoder
	EncodeStandard = EncodeZero | EncodeSlash | EncodeCtl | EncodeDel
	// Standard defines the encoding that is used for paths in- and output by rclone.
	//
	// List of replaced characters:
	//     (0x00)  -> '␀' // SYMBOL FOR NULL
	//   / (slash) -> '／' // FULLWIDTH SOLIDUS
	Standard = MultiEncoder(EncodeStandard)
)

// Possible flags for the MultiEncoder
const (
	EncodeZero        uint = 0         // NUL(0x00)
	EncodeSlash       uint = 1 << iota // /
	EncodeWin                          // :?"*<>|
	EncodeBackSlash                    // \
	EncodeHashPercent                  // #%
	EncodeDel                          // DEL(0x7F)
	EncodeCtl                          // CTRL(0x01-0x1F)
	EncodeLeftSpace                    // Leading SPACE
	EncodeLeftTilde                    // Leading ~
	EncodeRightSpace                   // Trailing SPACE
	EncodeRightPeriod                  // Trailing .
	EncodeInvalidUtf8                  // Invalid UTF-8 bytes
)

// Encoder can transform names to and from the original and translated version.
type Encoder interface {
	// Encode takes a raw name and substitutes any reserved characters and
	// patterns in it
	Encode(string) string
	// Decode takes a name and undoes any substitutions made by Encode
	Decode(string) string

	// FromStandardPath takes a / separated path in Standard encoding
	// and converts it to a / separated path in this encoding.
	FromStandardPath(string) string
	// FromStandardName takes name in Standard encoding and converts
	// it in this encoding.
	FromStandardName(string) string
	// ToStandardPath takes a / separated path in this encoding
	// and converts it to a / separated path in Standard encoding.
	ToStandardPath(string) string
	// ToStandardName takes name in this encoding and converts
	// it in Standard encoding.
	ToStandardName(string) string
}

// MultiEncoder is a configurable Encoder. The Encode* constants in this
// package can be combined using bitwise or (|) to enable handling of multiple
// character classes
type MultiEncoder uint

// Encode takes a raw name and substitutes any reserved characters and
// patterns in it
func (mask MultiEncoder) Encode(in string) string {
	var (
		encodeWin            = uint(mask)&EncodeWin != 0
		encodeSlash          = uint(mask)&EncodeSlash != 0
		encodeBackSlash      = uint(mask)&EncodeBackSlash != 0
		encodeHashPercent    = uint(mask)&EncodeHashPercent != 0
		encodeDel            = uint(mask)&EncodeDel != 0
		encodeCtl            = uint(mask)&EncodeCtl != 0
		encodeLeftSpace      = uint(mask)&EncodeLeftSpace != 0
		encodeLeftTilde      = uint(mask)&EncodeLeftTilde != 0
		encodeRightSpace     = uint(mask)&EncodeRightSpace != 0
		encodeRightPeriod    = uint(mask)&EncodeRightPeriod != 0
		encodeInvalidUnicode = uint(mask)&EncodeInvalidUtf8 != 0
	)

	// handle prefix only replacements
	prefix := ""
	if encodeLeftSpace && len(in) > 0 { // Leading SPACE
		if in[0] == ' ' {
			prefix, in = "␠", in[1:] // SYMBOL FOR SPACE
		} else if r, l := utf8.DecodeRuneInString(in); r == '␠' { // SYMBOL FOR SPACE
			prefix, in = string(QuoteRune)+"␠", in[l:] // SYMBOL FOR SPACE
		}
	}
	if encodeLeftTilde && len(in) > 0 { // Leading ~
		if in[0] == '~' {
			prefix, in = string('~'+fullOffset), in[1:] // FULLWIDTH TILDE
		} else if r, l := utf8.DecodeRuneInString(in); r == '~'+fullOffset {
			prefix, in = string(QuoteRune)+string('~'+fullOffset), in[l:] // FULLWIDTH TILDE
		}
	}
	// handle suffix only replacements
	suffix := ""
	if encodeRightSpace && len(in) > 0 { // Trailing SPACE
		if in[len(in)-1] == ' ' {
			suffix, in = "␠", in[:len(in)-1] // SYMBOL FOR SPACE
		} else if r, l := utf8.DecodeLastRuneInString(in); r == '␠' {
			suffix, in = string(QuoteRune)+"␠", in[:len(in)-l] // SYMBOL FOR SPACE
		}
	}
	if encodeRightPeriod && len(in) > 0 { // Trailing .
		if in[len(in)-1] == '.' {
			suffix, in = "．", in[:len(in)-1] // FULLWIDTH FULL STOP
		} else if r, l := utf8.DecodeLastRuneInString(in); r == '．' {
			suffix, in = string(QuoteRune)+"．", in[:len(in)-l] // FULLWIDTH FULL STOP
		}
	}
	index := 0
	if prefix == "" && suffix == "" {
		// find the first rune which (most likely) needs to be replaced
		index = strings.IndexFunc(in, func(r rune) bool {
			switch r {
			case 0, '␀', QuoteRune, utf8.RuneError:
				return true
			}
			if encodeWin { // :?"*<>|
				switch r {
				case '*', '<', '>', '?', ':', '|', '"',
					'＊', '＜', '＞', '？', '：', '｜', '＂':
					return true
				}
			}
			if encodeSlash { // /
				switch r {
				case '/',
					'／':
					return true
				}
			}
			if encodeBackSlash { // \
				switch r {
				case '\\',
					'＼':
					return true
				}
			}
			if encodeHashPercent { // #%
				switch r {
				case '#', '%',
					'＃', '％':
					return true
				}
			}
			if encodeDel { // DEL(0x7F)
				switch r {
				case rune(0x7F), '␡':
					return true
				}
			}
			if encodeCtl { // CTRL(0x01-0x1F)
				if r >= 1 && r <= 0x1F {
					return true
				} else if r > symbolOffset && r <= symbolOffset+0x1F {
					return true
				}
			}
			return false
		})
	}
	// nothing to replace, return input
	if index == -1 {
		return in
	}

	var out bytes.Buffer
	out.Grow(len(in) + len(prefix) + len(suffix))
	out.WriteString(prefix)
	// copy the clean part of the input and skip it
	out.WriteString(in[:index])
	in = in[index:]

	for i, r := range in {
		switch r {
		case 0:
			out.WriteRune(symbolOffset)
			continue
		case '␀', QuoteRune:
			out.WriteRune(QuoteRune)
			out.WriteRune(r)
			continue
		case utf8.RuneError:
			if encodeInvalidUnicode {
				// only encode invalid sequences and not utf8.RuneError
				if i+3 > len(in) || in[i:i+3] != string(utf8.RuneError) {
					_, l := utf8.DecodeRuneInString(in[i:])
					appendQuotedBytes(&out, in[i:i+l])
					continue
				}
			} else {
				// append the real bytes instead of utf8.RuneError
				_, l := utf8.DecodeRuneInString(in[i:])
				out.WriteString(in[i : i+l])
				continue
			}
		}
		if encodeWin { // :?"*<>|
			switch r {
			case '*', '<', '>', '?', ':', '|', '"':
				out.WriteRune(r + fullOffset)
				continue
			case '＊', '＜', '＞', '？', '：', '｜', '＂':
				out.WriteRune(QuoteRune)
				out.WriteRune(r)
				continue
			}
		}
		if encodeSlash { // /
			switch r {
			case '/':
				out.WriteRune(r + fullOffset)
				continue
			case '／':
				out.WriteRune(QuoteRune)
				out.WriteRune(r)
				continue
			}
		}
		if encodeBackSlash { // \
			switch r {
			case '\\':
				out.WriteRune(r + fullOffset)
				continue
			case '＼':
				out.WriteRune(QuoteRune)
				out.WriteRune(r)
				continue
			}
		}
		if encodeHashPercent { // #%
			switch r {
			case '#', '%':
				out.WriteRune(r + fullOffset)
				continue
			case '＃', '％':
				out.WriteRune(QuoteRune)
				out.WriteRune(r)
				continue
			}
		}
		if encodeDel { // DEL(0x7F)
			switch r {
			case rune(0x7F):
				out.WriteRune('␡') // SYMBOL FOR DELETE
				continue
			case '␡':
				out.WriteRune(QuoteRune)
				out.WriteRune(r)
				continue
			}
		}
		if encodeCtl { // CTRL(0x01-0x1F)
			if r >= 1 && r <= 0x1F {
				out.WriteRune('␀' + r) // SYMBOL FOR NULL
				continue
			} else if r > symbolOffset && r <= symbolOffset+0x1F {
				out.WriteRune(QuoteRune)
				out.WriteRune(r)
				continue
			}
		}
		out.WriteRune(r)
	}
	out.WriteString(suffix)
	return out.String()
}

// Decode takes a name and undoes any substitutions made by Encode
func (mask MultiEncoder) Decode(in string) string {
	var (
		encodeWin            = uint(mask)&EncodeWin != 0
		encodeSlash          = uint(mask)&EncodeSlash != 0
		encodeBackSlash      = uint(mask)&EncodeBackSlash != 0
		encodeHashPercent    = uint(mask)&EncodeHashPercent != 0
		encodeDel            = uint(mask)&EncodeDel != 0
		encodeCtl            = uint(mask)&EncodeCtl != 0
		encodeLeftSpace      = uint(mask)&EncodeLeftSpace != 0
		encodeLeftTilde      = uint(mask)&EncodeLeftTilde != 0
		encodeRightSpace     = uint(mask)&EncodeRightSpace != 0
		encodeRightPeriod    = uint(mask)&EncodeRightPeriod != 0
		encodeInvalidUnicode = uint(mask)&EncodeInvalidUtf8 != 0
	)

	// handle prefix only replacements
	prefix := ""
	if r, l1 := utf8.DecodeRuneInString(in); encodeLeftSpace && r == '␠' { // SYMBOL FOR SPACE
		prefix, in = " ", in[l1:]
	} else if encodeLeftTilde && r == '～' { // FULLWIDTH TILDE
		prefix, in = "~", in[l1:]
	} else if r == QuoteRune {
		if r, l2 := utf8.DecodeRuneInString(in[l1:]); encodeLeftSpace && r == '␠' { // SYMBOL FOR SPACE
			prefix, in = "␠", in[l1+l2:]
		} else if encodeLeftTilde && r == '～' { // FULLWIDTH TILDE
			prefix, in = "～", in[l1+l2:]
		}
	}

	// handle suffix only replacements
	suffix := ""
	if r, l := utf8.DecodeLastRuneInString(in); encodeRightSpace && r == '␠' { // SYMBOL FOR SPACE
		in = in[:len(in)-l]
		if r, l2 := utf8.DecodeLastRuneInString(in); r == QuoteRune {
			suffix, in = "␠", in[:len(in)-l2]
		} else {
			suffix = " "
		}
	} else if encodeRightPeriod && r == '．' { // FULLWIDTH FULL STOP
		in = in[:len(in)-l]
		if r, l2 := utf8.DecodeLastRuneInString(in); r == QuoteRune {
			suffix, in = "．", in[:len(in)-l2]
		} else {
			suffix = "."
		}
	}
	index := 0
	if prefix == "" && suffix == "" {
		// find the first rune which (most likely) needs to be replaced
		index = strings.IndexFunc(in, func(r rune) bool {
			switch r {
			case '␀', QuoteRune:
				return true
			}
			if encodeWin { // :?"*<>|
				switch r {
				case '＊', '＜', '＞', '？', '：', '｜', '＂':
					return true
				}
			}
			if encodeSlash { // /
				switch r {
				case '／':
					return true
				}
			}
			if encodeBackSlash { // \
				switch r {
				case '＼':
					return true
				}
			}
			if encodeHashPercent { // #%
				switch r {
				case '＃', '％':
					return true
				}
			}
			if encodeDel { // DEL(0x7F)
				switch r {
				case '␡':
					return true
				}
			}
			if encodeCtl { // CTRL(0x01-0x1F)
				if r > symbolOffset && r <= symbolOffset+0x1F {
					return true
				}
			}

			return false
		})
	}
	// nothing to replace, return input
	if index == -1 {
		return in
	}

	var out bytes.Buffer
	out.Grow(len(in))
	out.WriteString(prefix)
	// copy the clean part of the input and skip it
	out.WriteString(in[:index])
	in = in[index:]
	var unquote, unquoteNext, skipNext bool

	for i, r := range in {
		if skipNext {
			skipNext = false
			continue
		}
		unquote, unquoteNext = unquoteNext, false
		switch r {
		case '␀': // SYMBOL FOR NULL
			if unquote {
				out.WriteRune(r)
			} else {
				out.WriteRune(0)
			}
			continue
		case QuoteRune:
			if unquote {
				out.WriteRune(r)
			} else {
				unquoteNext = true
			}
			continue
		}
		if encodeWin { // :?"*<>|
			switch r {
			case '＊', '＜', '＞', '？', '：', '｜', '＂':
				if unquote {
					out.WriteRune(r)
				} else {
					out.WriteRune(r - fullOffset)
				}
				continue
			}
		}
		if encodeSlash { // /
			switch r {
			case '／': // FULLWIDTH SOLIDUS
				if unquote {
					out.WriteRune(r)
				} else {
					out.WriteRune(r - fullOffset)
				}
				continue
			}
		}
		if encodeBackSlash { // \
			switch r {
			case '＼': // FULLWIDTH REVERSE SOLIDUS
				if unquote {
					out.WriteRune(r)
				} else {
					out.WriteRune(r - fullOffset)
				}
				continue
			}
		}
		if encodeHashPercent { // #%
			switch r {
			case '＃', '％':
				if unquote {
					out.WriteRune(r)
				} else {
					out.WriteRune(r - fullOffset)
				}
				continue
			}
		}
		if encodeDel { // DEL(0x7F)
			switch r {
			case '␡': // SYMBOL FOR DELETE
				if unquote {
					out.WriteRune(r)
				} else {
					out.WriteRune(0x7F)
				}
				continue
			}
		}
		if encodeCtl { // CTRL(0x01-0x1F)
			if r > symbolOffset && r <= symbolOffset+0x1F {
				if unquote {
					out.WriteRune(r)
				} else {
					out.WriteRune(r - symbolOffset)
				}
				continue
			}
		}
		if unquote {
			if encodeInvalidUnicode {
				skipNext = appendUnquotedByte(&out, in[i:])
				if skipNext {
					continue
				}
			}
			out.WriteRune(QuoteRune)
		}
		switch r {
		case utf8.RuneError:
			// append the real bytes instead of utf8.RuneError
			_, l := utf8.DecodeRuneInString(in[i:])
			out.WriteString(in[i : i+l])
			continue
		}

		out.WriteRune(r)
	}
	if unquoteNext {
		out.WriteRune(QuoteRune)
	}
	out.WriteString(suffix)
	return out.String()
}

// FromStandardPath takes a / separated path in Standard encoding
// and converts it to a / separated path in this encoding.
func (mask MultiEncoder) FromStandardPath(s string) string {
	return FromStandardPath(mask, s)
}

// FromStandardName takes name in Standard encoding and converts
// it in this encoding.
func (mask MultiEncoder) FromStandardName(s string) string {
	return FromStandardName(mask, s)
}

// ToStandardPath takes a / separated path in this encoding
// and converts it to a / separated path in Standard encoding.
func (mask MultiEncoder) ToStandardPath(s string) string {
	return ToStandardPath(mask, s)
}

// ToStandardName takes name in this encoding and converts
// it in Standard encoding.
func (mask MultiEncoder) ToStandardName(s string) string {
	return ToStandardName(mask, s)
}

func appendQuotedBytes(w io.Writer, s string) {
	for _, b := range []byte(s) {
		_, _ = fmt.Fprintf(w, string(QuoteRune)+"%02X", b)
	}
}
func appendUnquotedByte(w io.Writer, s string) bool {
	if len(s) < 2 {
		return false
	}
	u, err := strconv.ParseUint(s[:2], 16, 8)
	if err != nil {
		return false
	}
	n, _ := w.Write([]byte{byte(u)})
	return n == 1
}

type identity struct{}

func (identity) Encode(in string) string { return in }
func (identity) Decode(in string) string { return in }

func (i identity) FromStandardPath(s string) string {
	return FromStandardPath(i, s)
}
func (i identity) FromStandardName(s string) string {
	return FromStandardName(i, s)
}
func (i identity) ToStandardPath(s string) string {
	return ToStandardPath(i, s)
}
func (i identity) ToStandardName(s string) string {
	return ToStandardName(i, s)
}

// Identity returns a Encoder that always returns the input value
func Identity() Encoder {
	return identity{}
}

// FromStandardPath takes a / separated path in Standard encoding
// and converts it to a / separated path in the given encoding.
func FromStandardPath(e Encoder, s string) string {
	if e == Standard {
		return s
	}
	parts := strings.Split(s, "/")
	encoded := make([]string, len(parts))
	changed := false
	for i, p := range parts {
		enc := FromStandardName(e, p)
		changed = changed || enc != p
		encoded[i] = enc
	}
	if !changed {
		return s
	}
	return strings.Join(encoded, "/")
}

// FromStandardName takes name in Standard encoding and converts
// it in the given encoding.
func FromStandardName(e Encoder, s string) string {
	if e == Standard {
		return s
	}
	return e.Encode(Standard.Decode(s))
}

// ToStandardPath takes a / separated path in the given encoding
// and converts it to a / separated path in Standard encoding.
func ToStandardPath(e Encoder, s string) string {
	if e == Standard {
		return s
	}
	parts := strings.Split(s, "/")
	encoded := make([]string, len(parts))
	changed := false
	for i, p := range parts {
		dec := ToStandardName(e, p)
		changed = changed || dec != p
		encoded[i] = dec
	}
	if !changed {
		return s
	}
	return strings.Join(encoded, "/")
}

// ToStandardName takes name in the given encoding and converts
// it in Standard encoding.
func ToStandardName(e Encoder, s string) string {
	if e == Standard {
		return s
	}
	return Standard.Encode(e.Decode(s))
}
