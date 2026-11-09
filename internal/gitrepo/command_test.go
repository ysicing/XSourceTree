package gitrepo

import (
	"io"
	"strings"
	"testing"
)

func TestLimitedBufferBoundsStreamCopies(t *testing.T) {
	var output limitedBuffer
	input := struct{ io.Reader }{strings.NewReader(strings.Repeat("x", outputLimit+1))}
	copied, err := io.Copy(&output, input)
	if err != nil || copied != outputLimit+1 {
		t.Fatalf("stream copy: %d, %v", copied, err)
	}
	if !output.overflow || len(output.String()) != outputLimit {
		t.Fatalf("output limit bypassed: overflow=%t, size=%d", output.overflow, len(output.String()))
	}
}
