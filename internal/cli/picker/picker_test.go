package picker

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	if p == nil {
		t.Fatal("New returned nil")
	}
}

func TestChoose_EmptyOptions(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.choose(context.Background(), "Select model", nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v, want 'no options'", err)
	}
}

func TestSelectTool_EmptyOptions(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.SelectTool(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectModel_EmptyOptions(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.SelectModel(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectSource_EmptyAllowed(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.SelectSource(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no sources") {
		t.Fatalf("err = %v, want 'no sources'", err)
	}
}

func TestPickMarkdownInCwd_NoFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.PickMarkdownInCwd(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no markdown files") {
		t.Fatalf("err = %v, want 'no markdown files'", err)
	}
}

func TestErrEmptyFromFile(t *testing.T) {
	if ErrEmptyFromFile == nil {
		t.Fatal("ErrEmptyFromFile sentinel must not be nil")
	}
	if !strings.Contains(ErrEmptyFromFile.Error(), "no markdown") {
		t.Fatalf("error = %q, want to mention 'no markdown'", ErrEmptyFromFile.Error())
	}
}
