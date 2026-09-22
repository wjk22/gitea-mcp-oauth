package repo

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"unicode/utf8"
)

const (
	DefaultMaxBytes = 32768
	MaxMaxBytes     = 262144
)

// ShapeOptions configures file reading transformations.
type ShapeOptions struct {
	StartLine *int
	EndLine   *int
	MaxBytes  *int
	WithLines bool
}

// ShapedFile holds the shaped output of a file read.
type ShapedFile struct {
	Binary        bool
	Content       string
	TotalLines    int
	StartLine     int
	EndLine       int
	Truncated     bool
	NextStartLine int
}

type lineInfo struct {
	start int
	end   int
}

// ShapeFileContent performs base64 decoding, binary detection, line range selection,
// size cap enforcement, and optional line numbering on raw file content.
func ShapeFileContent(raw []byte, opts ShapeOptions) (*ShapedFile, error) {
	clean := bytes.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, raw)
	decoded, err := base64.StdEncoding.DecodeString(string(clean))
	if err != nil {
		return nil, fmt.Errorf("decode base64 content err: %w", err)
	}

	// FR-2: binary if first 8000 bytes contain NUL byte or content is invalid UTF-8
	checkLen := min(len(decoded), 8000)
	if bytes.IndexByte(decoded[:checkLen], 0) != -1 || !utf8.Valid(decoded) {
		return &ShapedFile{Binary: true}, nil
	}

	// Parameter validation
	if opts.StartLine != nil && *opts.StartLine < 1 {
		return nil, fmt.Errorf("start_line must be greater than or equal to 1 (got %d)", *opts.StartLine)
	}
	if opts.MaxBytes != nil && *opts.MaxBytes < 1 {
		return nil, fmt.Errorf("max_bytes must be at least 1 (got %d)", *opts.MaxBytes)
	}
	if opts.StartLine != nil && opts.EndLine != nil && *opts.EndLine < *opts.StartLine {
		return nil, fmt.Errorf("end_line (%d) cannot be less than start_line (%d)", *opts.EndLine, *opts.StartLine)
	}

	// FR-3: empty file returns total_lines: 0 and empty content without error
	if len(decoded) == 0 {
		return &ShapedFile{
			Content:    "",
			TotalLines: 0,
			StartLine:  0,
			EndLine:    0,
		}, nil
	}

	// Split into lines; a trailing newline does not add an extra line
	var lines []lineInfo
	n := len(decoded)
	lineStart := 0
	for i := range n {
		if decoded[i] == '\n' {
			lines = append(lines, lineInfo{start: lineStart, end: i + 1})
			lineStart = i + 1
		}
	}
	if lineStart < n {
		lines = append(lines, lineInfo{start: lineStart, end: n})
	}
	totalLines := len(lines)

	startLine := 1
	if opts.StartLine != nil {
		startLine = *opts.StartLine
	}
	if startLine > totalLines {
		return nil, fmt.Errorf("start_line (%d) exceeds total lines (%d)", startLine, totalLines)
	}

	endLine := totalLines
	if opts.EndLine != nil {
		endLine = *opts.EndLine
	}
	if endLine < startLine {
		return nil, fmt.Errorf("end_line (%d) cannot be less than start_line (%d)", endLine, startLine)
	}
	if endLine > totalLines {
		endLine = totalLines
	}

	maxBytes := DefaultMaxBytes
	if opts.MaxBytes != nil {
		maxBytes = *opts.MaxBytes
	}
	if maxBytes > MaxMaxBytes {
		maxBytes = MaxMaxBytes
	}

	var buf bytes.Buffer
	lastReturnedLine := startLine - 1

	for idx := startLine - 1; idx < endLine; idx++ {
		var lineBytes []byte
		if opts.WithLines {
			lineBytes = []byte(fmt.Sprintf("%d\t%s", idx+1, string(decoded[lines[idx].start:lines[idx].end])))
		} else {
			lineBytes = decoded[lines[idx].start:lines[idx].end]
		}

		if buf.Len()+len(lineBytes) <= maxBytes {
			buf.Write(lineBytes)
			lastReturnedLine = idx + 1
		} else {
			// First line alone exceeds cap: cut at valid UTF-8 boundary and omit next_start_line
			if buf.Len() == 0 {
				cutoff := min(len(lineBytes), maxBytes)
				for cutoff > 0 && !utf8.Valid(lineBytes[:cutoff]) {
					cutoff--
				}
				buf.Write(lineBytes[:cutoff])
				return &ShapedFile{
					Content:       buf.String(),
					TotalLines:    totalLines,
					StartLine:     startLine,
					EndLine:       startLine,
					Truncated:     true,
					NextStartLine: 0,
				}, nil
			}

			// Cut after last complete line that fits
			return &ShapedFile{
				Content:       buf.String(),
				TotalLines:    totalLines,
				StartLine:     startLine,
				EndLine:       lastReturnedLine,
				Truncated:     true,
				NextStartLine: idx + 1,
			}, nil
		}
	}

	return &ShapedFile{
		Content:       buf.String(),
		TotalLines:    totalLines,
		StartLine:     startLine,
		EndLine:       endLine,
		Truncated:     false,
		NextStartLine: 0,
	}, nil
}

// FormatFileContentResult constructs the tool response map from SDK metadata and shaped content.
func FormatFileContentResult(name, path, sha, fileType string, size int64, shaped *ShapedFile) map[string]any {
	result := map[string]any{
		"name": name,
		"path": path,
		"sha":  sha,
		"type": fileType,
		"size": size,
	}
	if shaped.Binary {
		result["binary"] = true
		return result
	}
	result["content"] = shaped.Content
	result["total_lines"] = shaped.TotalLines
	result["start_line"] = shaped.StartLine
	result["end_line"] = shaped.EndLine
	if shaped.Truncated {
		result["truncated"] = true
		if shaped.NextStartLine > 0 {
			result["next_start_line"] = shaped.NextStartLine
		}
	}
	return result
}
