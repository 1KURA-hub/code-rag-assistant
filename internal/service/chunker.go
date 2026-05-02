package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"code-rag-assistant/internal/util"
)

type Chunk struct {
	StartLine  int
	EndLine    int
	Content    string
	Language   string
	SymbolName string
	SymbolType string
}

func ChunkSourceFile(file util.SourceFile, maxLines, overlap int) []Chunk {
	if strings.EqualFold(filepath.Ext(file.Path), ".go") {
		chunks := chunkGoFile(file, maxLines)
		if len(chunks) > 0 {
			return chunks
		}
	}
	return ChunkContent(file.Content, maxLines, overlap)
}

func ChunkContent(content string, maxLines, overlap int) []Chunk {
	lines := strings.Split(content, "\n")
	if maxLines <= 0 {
		maxLines = 80
	}
	if overlap < 0 || overlap >= maxLines {
		overlap = 0
	}
	var chunks []Chunk
	for start := 0; start < len(lines); {
		end := start + maxLines
		if end > len(lines) {
			end = len(lines)
		}
		chunkText := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if chunkText != "" {
			chunks = append(chunks, Chunk{
				StartLine: start + 1,
				EndLine:   end,
				Content:   chunkText,
			})
		}
		if end == len(lines) {
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}

func chunkGoFile(file util.SourceFile, maxLines int) []Chunk {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file.Path, file.Content, parser.ParseComments)
	if err != nil {
		return nil
	}
	lines := strings.Split(file.Content, "\n")
	var chunks []Chunk
	for _, decl := range parsed.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = receiverName(d.Recv.List[0].Type) + "." + name
			}
			chunks = append(chunks, goNodeChunk(fset, lines, d.Pos(), d.End(), name, "function", maxLines)...)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					chunks = append(chunks, goNodeChunk(fset, lines, s.Pos(), s.End(), s.Name.Name, "type", maxLines)...)
				case *ast.ValueSpec:
					symbolType := valueSpecSymbolType(d.Tok)
					if symbolType == "" {
						continue
					}
					chunks = append(chunks, goNodeChunk(fset, lines, s.Pos(), s.End(), valueSpecNames(s), symbolType, maxLines)...)
				}
			}
		}
	}
	return chunks
}

func valueSpecSymbolType(tok token.Token) string {
	switch tok {
	case token.CONST:
		return "constant"
	case token.VAR:
		return "variable"
	default:
		return ""
	}
}

func valueSpecNames(spec *ast.ValueSpec) string {
	names := make([]string, 0, len(spec.Names))
	for _, name := range spec.Names {
		if name.Name != "" {
			names = append(names, name.Name)
		}
	}
	return strings.Join(names, ",")
}

func goNodeChunk(fset *token.FileSet, lines []string, startPos, endPos token.Pos, symbolName, symbolType string, maxLines int) []Chunk {
	start := fset.Position(startPos).Line
	end := fset.Position(endPos).Line
	if start <= 0 || end < start || start > len(lines) {
		return nil
	}
	if end > len(lines) {
		end = len(lines)
	}
	text := strings.TrimSpace(strings.Join(lines[start-1:end], "\n"))
	if text == "" {
		return nil
	}
	if maxLines <= 0 || end-start+1 <= maxLines {
		return []Chunk{{
			StartLine:  start,
			EndLine:    end,
			Content:    text,
			Language:   "go",
			SymbolName: symbolName,
			SymbolType: symbolType,
		}}
	}

	var chunks []Chunk
	for chunkStart := start; chunkStart <= end; chunkStart += maxLines {
		chunkEnd := chunkStart + maxLines - 1
		if chunkEnd > end {
			chunkEnd = end
		}
		chunkText := strings.TrimSpace(strings.Join(lines[chunkStart-1:chunkEnd], "\n"))
		if chunkText != "" {
			chunks = append(chunks, Chunk{
				StartLine:  chunkStart,
				EndLine:    chunkEnd,
				Content:    chunkText,
				Language:   "go",
				SymbolName: symbolName,
				SymbolType: symbolType,
			})
		}
	}
	return chunks
}

func receiverName(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return receiverName(v.X)
	case *ast.IndexExpr:
		return receiverName(v.X)
	case *ast.IndexListExpr:
		return receiverName(v.X)
	default:
		return "receiver"
	}
}
