// This package provides a Lexer that functions similarly to Rob Pike's discussion
// about lexer design in this [talk](https://www.youtube.com/watch?v=HxaD_trXwRE).
//
// You can define your token types by using the `lexer.TokenType` type (`int`) via
//
//     const (
//             StringToken lexer.TokenType = iota
//             IntegerToken
//             // etc...
//     )
//
// And then you define your own state functions (`lexer.StateFunc`) to handle
// analyzing the string.
//
//     func StringState(l *lexer.L) lexer.StateFunc {
//             l.Next() // eat starting "
//             l.Ignore() // drop current value
//             while l.Peek() != '"' {
//                     l.Next()
//             }
//             l.Emit(StringToken)
//
//             return SomeStateFunction
//     }
//
// This Lexer is meant to emit tokens in such a fashion that it can be consumed
// by go yacc.
package lexer

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type StateFunc func(*L) StateFunc

type TokenType int

const (
	EOFRune    rune      = -1
	EmptyToken TokenType = 0
)

type Token struct {
	Type  TokenType
	Value string
}

type L struct {
	source          string
	start, position int
	startState      StateFunc
	Err             error
	tokens          chan Token
	ErrorHandler    func(e string)
	rewind          runeStack
	builder         *strings.Builder
}

// New creates a returns a lexer ready to parse the given source code.
func New(src string, start StateFunc) *L {
	var builder = new(strings.Builder)
	builder.WriteString(src)

	return &L{
		source:     builder.String(),
		startState: start,
		start:      0,
		position:   0,
		rewind:     newRuneStack(),
		builder:    builder,
	}
}

// Start begins executing the Lexer in an asynchronous manner (using a goroutine).
func (l *L) Start() {
	// Take half the string length as a buffer size.
	buffSize := len(l.source) / 2
	if buffSize <= 0 {
		buffSize = 1
	}
	l.tokens = make(chan Token, buffSize)
	go l.run()
}

func (l *L) StartSync() {
	// Take half the string length as a buffer size.
	buffSize := len(l.source) / 2
	if buffSize <= 0 {
		buffSize = 1
	}
	l.tokens = make(chan Token, buffSize)
	l.run()
}

// Current returns the value being being analyzed at this moment.
func (l *L) Current() string {
	return l.source[l.start:l.position]
}

// Emit will receive a token type and push a new token with the current analyzed
// value into the tokens channel.
func (l *L) Emit(t TokenType) {
	tok := Token{
		Type:  t,
		Value: l.Current(),
	}
	l.tokens <- tok
	l.start = l.position
	l.rewind.clear()
}

// Ignore clears the rewind stack and then sets the current beginning position
// to the current position in the source which effectively ignores the section
// of the source being analyzed.
func (l *L) Ignore() {
	l.rewind.clear()
	l.start = l.position
}

// Peek performs a Next operation immediately followed by a Rewind returning the
// peeked rune.
func (l *L) Peek() rune {
	r := l.Next()
	l.Rewind()

	return r
}

// Rewind will take the last rune read (if any) and rewind back. Rewinds can
// occur more than once per call to Next but you can never rewind past the
// last point a token was emitted.
func (l *L) Rewind() {
	r := l.rewind.pop()
	if r > EOFRune {
		size := utf8.RuneLen(r)
		l.position -= size
		if l.position < l.start {
			l.position = l.start
		}
	}
}

// Next pulls the next rune from the Lexer and returns it, moving the position
// forward in the source.
func (l *L) Next() rune {
	var (
		r rune
		s int
	)
	str := l.source[l.position:]
	if len(str) == 0 {
		r, s = EOFRune, 0
	} else {
		r, s = utf8.DecodeRuneInString(str)
	}
	l.position += s
	l.rewind.push(r)

	return r
}

// Take receives a string containing all acceptable strings and will contine
// over each consecutive character in the source until a token not in the given
// string is encountered. This should be used to quickly pull token parts.
func (l *L) Take(chars string) {
	r := l.Next()
	for strings.ContainsRune(chars, r) {
		r = l.Next()
	}
	l.Rewind() // last next wasn't a match
}

// NextToken returns the next token from the lexer and a value to denote whether
// or not the token is finished.
func (l *L) NextToken() (*Token, bool) {
	if tok, ok := <-l.tokens; ok {
		return &tok, false
	} else {
		return nil, true
	}
}

// Partial yyLexer implementation

func (l *L) Error(e string) {
	if l.ErrorHandler != nil {
		l.Err = errors.New(e)
		l.ErrorHandler(e)
	} else {
		panic(e)
	}
}

// Append adds new string to the source string with string builder.
func (l *L) Append(more string) {
	l.builder.WriteString(more)
	l.source = l.builder.String()
}

// NextPeek calls Next() and Peek() consecutively.
func (l *L) NextPeek() rune {
	l.Next()
	return l.Peek()
}

// Private methods

func (l *L) run() {
	state := l.startState
	for state != nil {
		state = state(l)
	}
	close(l.tokens)
}
