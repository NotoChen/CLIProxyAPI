package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

type sourceFile struct {
	path    string
	fset    *token.FileSet
	file    *ast.File
	changed bool
}

func loadSource(path string) (*sourceFile, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &sourceFile{path: path, fset: fset, file: file}, nil
}

func (s *sourceFile) save() error {
	if !s.changed {
		return nil
	}
	var output bytes.Buffer
	if err := format.Node(&output, s.fset, s.file); err != nil {
		return fmt.Errorf("format %s: %w", s.path, err)
	}
	info, err := os.Stat(s.path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", s.path, err)
	}
	if errWrite := os.WriteFile(s.path, output.Bytes(), info.Mode()); errWrite != nil {
		return fmt.Errorf("write %s: %w", s.path, errWrite)
	}
	return nil
}

func findFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func findStruct(root ast.Node, name string) *ast.StructType {
	var result *ast.StructType
	ast.Inspect(root, func(node ast.Node) bool {
		if result != nil {
			return false
		}
		typeSpec, ok := node.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != name {
			return true
		}
		result, _ = typeSpec.Type.(*ast.StructType)
		return false
	})
	return result
}

func fieldIndex(structType *ast.StructType, name string) int {
	for index, field := range structType.Fields.List {
		for _, fieldName := range field.Names {
			if fieldName.Name == name {
				return index
			}
		}
	}
	return -1
}

func insertFieldAfter(structType *ast.StructType, after string, field *ast.Field) error {
	if fieldIndex(structType, field.Names[0].Name) >= 0 {
		return nil
	}
	afterIndex := fieldIndex(structType, after)
	if afterIndex < 0 {
		return fmt.Errorf("anchor field %s not found", after)
	}
	fields := make([]*ast.Field, 0, len(structType.Fields.List)+1)
	fields = append(fields, structType.Fields.List[:afterIndex+1]...)
	fields = append(fields, field)
	fields = append(fields, structType.Fields.List[afterIndex+1:]...)
	structType.Fields.List = fields
	return nil
}

func displayNameField(pointer bool, yamlTag bool) *ast.Field {
	typeExpr := ast.Expr(ast.NewIdent("string"))
	if pointer {
		typeExpr = &ast.StarExpr{X: ast.NewIdent("string")}
	}
	tag := "`json:\"display-name\"`"
	if yamlTag {
		tag = "`yaml:\"display-name,omitempty\" json:\"display-name,omitempty\"`"
	}
	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent("DisplayName")},
		Type:  typeExpr,
		Tag:   &ast.BasicLit{Kind: token.STRING, Value: tag},
	}
}

func selectorPath(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		prefix := selectorPath(value.X)
		if prefix == "" {
			return value.Sel.Name
		}
		return prefix + "." + value.Sel.Name
	case *ast.ParenExpr:
		return selectorPath(value.X)
	case *ast.StarExpr:
		return selectorPath(value.X)
	default:
		return ""
	}
}

func assignsPath(root ast.Node, target string) bool {
	found := false
	ast.Inspect(root, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assignment.Lhs {
			if selectorPath(lhs) == target {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

func directlyAssignsPath(statement ast.Stmt, target string) bool {
	assignment, ok := statement.(*ast.AssignStmt)
	if !ok {
		return false
	}
	for _, lhs := range assignment.Lhs {
		if selectorPath(lhs) == target {
			return true
		}
	}
	return false
}

func pathExpression(path string) ast.Expr {
	parts := strings.Split(path, ".")
	var expression ast.Expr = ast.NewIdent(parts[0])
	for _, part := range parts[1:] {
		expression = &ast.SelectorExpr{X: expression, Sel: ast.NewIdent(part)}
	}
	return expression
}

func displayNamePatchStatement() ast.Stmt {
	value := pathExpression("body.Value.DisplayName")
	return &ast.IfStmt{
		Cond: &ast.BinaryExpr{X: value, Op: token.NEQ, Y: ast.NewIdent("nil")},
		Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.AssignStmt{
				Lhs: []ast.Expr{pathExpression("entry.DisplayName")},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{&ast.CallExpr{
					Fun:  pathExpression("strings.TrimSpace"),
					Args: []ast.Expr{&ast.StarExpr{X: pathExpression("body.Value.DisplayName")}},
				}},
			},
		}},
	}
}

func normalizationStatement(target string) ast.Stmt {
	return &ast.AssignStmt{
		Lhs: []ast.Expr{pathExpression(target)},
		Tok: token.ASSIGN,
		Rhs: []ast.Expr{&ast.CallExpr{
			Fun:  pathExpression("strings.TrimSpace"),
			Args: []ast.Expr{pathExpression(target)},
		}},
	}
}

func insertAfterMatchingStatement(root ast.Node, matches func(ast.Stmt) bool, statement ast.Stmt) bool {
	inserted := false
	ast.Inspect(root, func(node ast.Node) bool {
		if inserted {
			return false
		}
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for index, existing := range block.List {
			if !matches(existing) {
				continue
			}
			statements := make([]ast.Stmt, 0, len(block.List)+1)
			statements = append(statements, block.List[:index+1]...)
			statements = append(statements, statement)
			statements = append(statements, block.List[index+1:]...)
			block.List = statements
			inserted = true
			return false
		}
		return true
	})
	return inserted
}

func ensurePatchFunction(file *sourceFile, functionName, patchTypeName string) error {
	fn := findFunc(file.file, functionName)
	if fn == nil {
		return fmt.Errorf("%s: function %s not found", file.path, functionName)
	}
	patchStruct := findStruct(fn, patchTypeName)
	if patchStruct == nil {
		return fmt.Errorf("%s: patch struct %s not found in %s", file.path, patchTypeName, functionName)
	}
	if fieldIndex(patchStruct, "DisplayName") < 0 {
		if err := insertFieldAfter(patchStruct, "APIKey", displayNameField(true, false)); err != nil {
			return fmt.Errorf("%s: %s: %w", file.path, functionName, err)
		}
		file.changed = true
	}
	if assignsPath(fn, "entry.DisplayName") {
		return nil
	}
	statement := displayNamePatchStatement()
	inserted := insertAfterMatchingStatement(fn.Body, func(existing ast.Stmt) bool {
		ifStatement, ok := existing.(*ast.IfStmt)
		return ok && assignsPath(ifStatement.Body, "entry.APIKey")
	}, statement)
	if !inserted {
		return fmt.Errorf("%s: APIKey patch anchor not found in %s", file.path, functionName)
	}
	file.changed = true
	return nil
}

func ensureTopLevelField(file *sourceFile, typeName string) error {
	structType := findStruct(file.file, typeName)
	if structType == nil {
		return fmt.Errorf("%s: struct %s not found", file.path, typeName)
	}
	if fieldIndex(structType, "DisplayName") >= 0 {
		return nil
	}
	field := displayNameField(false, true)
	field.Doc = &ast.CommentGroup{List: []*ast.Comment{{Text: "// DisplayName is an optional human-readable name for this provider credential."}}}
	if err := insertFieldAfter(structType, "APIKey", field); err != nil {
		return fmt.Errorf("%s: %s: %w", file.path, typeName, err)
	}
	file.changed = true
	return nil
}

func ensureNormalization(file *sourceFile, functionName, target, anchor string) error {
	fn := findFunc(file.file, functionName)
	if fn == nil {
		return fmt.Errorf("%s: function %s not found", file.path, functionName)
	}
	if assignsPath(fn, target) {
		return nil
	}
	statement := normalizationStatement(target)
	inserted := insertAfterMatchingStatement(fn.Body, func(existing ast.Stmt) bool {
		return directlyAssignsPath(existing, anchor)
	}, statement)
	if !inserted {
		return fmt.Errorf("%s: normalization anchor %s not found in %s", file.path, anchor, functionName)
	}
	file.changed = true
	return nil
}

func updateManagementHandlers() error {
	file, err := loadSource("internal/api/handlers/management/config_lists.go")
	if err != nil {
		return err
	}
	patches := []struct {
		functionName string
		patchType    string
	}{
		{"PatchGeminiKey", "geminiKeyPatch"},
		{"PatchInteractionsKey", "geminiKeyPatch"},
		{"PatchClaudeKey", "claudeKeyPatch"},
		{"PatchVertexCompatKey", "vertexCompatPatch"},
		{"PatchCodexKey", "codexKeyPatch"},
		{"PatchXAIKey", "xaiKeyPatch"},
	}
	for _, patch := range patches {
		if errPatch := ensurePatchFunction(file, patch.functionName, patch.patchType); errPatch != nil {
			return errPatch
		}
	}
	return file.save()
}

func updateConfigTypes() error {
	file, err := loadSource("internal/config/config_types.go")
	if err != nil {
		return err
	}
	for _, typeName := range []string{"ClaudeKey", "CodexKey", "GeminiKey"} {
		if errField := ensureTopLevelField(file, typeName); errField != nil {
			return errField
		}
	}
	return file.save()
}

func updateConfigNormalization() error {
	file, err := loadSource("internal/config/config_normalization.go")
	if err != nil {
		return err
	}
	normalizations := []struct {
		functionName string
		target       string
		anchor       string
	}{
		{"sanitizeCodexKeyEntries", "e.DisplayName", "e.Prefix"},
		{"SanitizeClaudeKeys", "entry.DisplayName", "entry.Prefix"},
		{"sanitizeGeminiKeyEntries", "entry.DisplayName", "entry.APIKey"},
	}
	for _, normalization := range normalizations {
		if errNormalization := ensureNormalization(file, normalization.functionName, normalization.target, normalization.anchor); errNormalization != nil {
			return errNormalization
		}
	}
	return file.save()
}

func updateVertexCompat() error {
	file, err := loadSource("internal/config/vertex_compat.go")
	if err != nil {
		return err
	}
	if errField := ensureTopLevelField(file, "VertexCompatKey"); errField != nil {
		return errField
	}
	if errNormalization := ensureNormalization(file, "SanitizeVertexCompatKeys", "entry.DisplayName", "entry.APIKey"); errNormalization != nil {
		return errNormalization
	}
	return file.save()
}

func main() {
	updates := []func() error{
		updateManagementHandlers,
		updateConfigTypes,
		updateConfigNormalization,
		updateVertexCompat,
	}
	for _, update := range updates {
		if err := update(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
