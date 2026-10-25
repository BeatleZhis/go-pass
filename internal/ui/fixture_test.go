package ui

import (
	"testing"

	"github.com/beatlezhis/go-pass/internal/pass"
)

func secretFixture() *pass.Secret {
	return pass.ParseSecret("demo", "p@ss\nlogin: user\nurl: https://example.com\n\nnote line\n")
}

func TestSecretFixtureParsing(t *testing.T) {
	s := secretFixture()
	if s.Password != "p@ss" {
		t.Fatalf("Password = %q", s.Password)
	}
	if len(s.Fields) != 2 {
		t.Fatalf("Fields = %+v", s.Fields)
	}
	if s.Notes != "note line" {
		t.Fatalf("Notes = %q", s.Notes)
	}
}
