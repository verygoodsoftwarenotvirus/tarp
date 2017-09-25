package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"log"
	"os"
	"strings"

	"github.com/fatih/set"
	"go/token"
)

func parseExpr(in ast.Expr, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	// FIXME: iterate over in.Args to see if there are function calls
	switch f := in.(type) {
	case *ast.Ident:
		functionName := f.Name
		if _, ok := helperFunctionReturnMap[functionName]; !ok {
			out.Add(functionName)
		}
	case *ast.SelectorExpr:
		if x, ok := f.X.(*ast.Ident); ok {
			structVarName := x.Name
			calledMethodName := f.Sel.Name
			if _, ok := nameToTypeMap[structVarName]; ok {
				out.Add(fmt.Sprintf("%s.%s", nameToTypeMap[structVarName], calledMethodName))
			}
		}
	case *ast.FuncLit:
		parseFuncLit(f, nameToTypeMap, helperFunctionReturnMap, out)
	}
}

func parseCallExpr(in *ast.CallExpr, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	for _, a := range in.Args {
		if r, ok := a.(*ast.CallExpr); ok {
			parseCallExpr(r, nameToTypeMap, helperFunctionReturnMap, out)
		}
	}
	parseExpr(in.Fun, nameToTypeMap, helperFunctionReturnMap, out)
}

// parseUnaryExpr parses Unary expressions. From the go/ast docs:
//      A UnaryExpr node represents a unary expression. Unary "*" expressions are represented via StarExpr nodes.
// (handles declarations like `callExpr := &ast.UnaryExpr{}` or `callExpr := ast.UnaryExpr{}`)
func parseUnaryExpr(in *ast.UnaryExpr, varName string, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	if cl, ok := in.X.(*ast.CompositeLit); ok {
		for _, e := range cl.Elts {
			switch et := e.(type) {
			case *ast.CallExpr:
				parseExpr(et.Fun, nameToTypeMap, helperFunctionReturnMap, out)
			case *ast.KeyValueExpr:
				if vt, ok := et.Value.(*ast.CallExpr); ok {
					parseCallExpr(vt, nameToTypeMap, helperFunctionReturnMap, out)
				}
			}
		}
		switch u := cl.Type.(type) {
		case *ast.Ident:
			nameToTypeMap[varName] = u.Name
		case *ast.SelectorExpr:
			nameToTypeMap[varName] = u.Sel.Name
		}
	}
}

// parseDeclStmt parses declaration statments. From the go/ast docs:
// 		A DeclStmt node represents a declaration in a statement list.
// DeclStmts come from function bodies, GenDecls come from package-wide const or var declarations
func parseDeclStmt(in *ast.DeclStmt, nameToTypeMap map[string]string) {
	// FIXME: we make a whole mess of assumptions right here. I haven't thusfar seen any
	// 		  evidence that these assumptions are incorrect or dangerous, but that doesn't
	// 		  mean they don't carry the inherent risk that most assumptions do.
	if s, ok := in.Decl.(*ast.GenDecl).Specs[0].(*ast.ValueSpec); ok {
		varName := s.Names[0].Name
		switch t := s.Type.(type) {
		case *ast.Ident:
			nameToTypeMap[varName] = t.Name
		case *ast.SelectorExpr:
			nameToTypeMap[varName] = t.Sel.Name
		}
	}
}

// parseExprStmt parses expression statements. From the go/ast docs:
// 		An ExprStmt node represents a (stand-alone) expression in a statement list.
func parseExprStmt(in *ast.ExprStmt, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	if c, ok := in.X.(*ast.CallExpr); ok {
		parseCallExpr(c, nameToTypeMap, helperFunctionReturnMap, out)
	}
}

func parseCompositeLit(in *ast.CompositeLit, varName string, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	for _, e := range in.Elts {
		if et, ok := e.(*ast.CallExpr); ok {
			parseExpr(et.Fun, nameToTypeMap, helperFunctionReturnMap, out)
		}
	}

	switch t := in.Type.(type) {
	case *ast.Ident:
		nameToTypeMap[varName] = t.Name
	case *ast.SelectorExpr:
		nameToTypeMap[varName] = t.Sel.Name
	}
}

// parseGenDecl handles GenDecl nodes. From the go/ast docs:
//     A GenDecl node (generic declaration node) represents an import, constant, type or variable declaration.
func parseGenDecl(in *ast.GenDecl, nameToTypeMap map[string]string) {
	for _, spec := range in.Specs {
		if global, ok := spec.(*ast.ValueSpec); ok {
			varName := global.Names[0].Name
			if global.Type != nil {
				if t, ok := global.Type.(*ast.Ident); ok {
					typeName := t.Name
					nameToTypeMap[varName] = typeName
				}
			}
		}
	}
}

// parseFuncDecl parses function declarations. From the go/ast docs:
//		A FuncDecl node represents a function declaration.
// the main purpose of this is to parse functions that are declared in non-test go files.
func parseFuncDecl(f *ast.FuncDecl) string {
	functionName := f.Name.Name // "Avoid Stutter" lol
	var parentName string
	if f.Recv != nil {
		switch x := f.Recv.List[0].Type.(type) {
		case *ast.StarExpr:
			if parent, ok := x.X.(*ast.Ident); ok {
				parentName = parent.Name
			}
		case *ast.Ident:
			parentName = x.Obj.Name
		}
	}

	if parentName != "" {
		return fmt.Sprintf("%s.%s", parentName, functionName)
	}
	return functionName

}

// parseAssignStmt handles AssignStmt nodes. From the go/ast docs:
//    An AssignStmt node represents an assignment or a short variable declaration
func parseAssignStmt(in *ast.AssignStmt, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	leftHandSide := []string{}
	for i := range in.Lhs {
		if l, ok := in.Lhs[i].(*ast.Ident); ok {
			varName := l.Name
			leftHandSide = append(leftHandSide, varName)
		}
	}

	for j := range in.Rhs {
		switch t := in.Rhs[j].(type) {
		case *ast.FuncLit:
			parseFuncLit(t, nameToTypeMap, helperFunctionReturnMap, out)
		case *ast.UnaryExpr:
			parseUnaryExpr(t, leftHandSide[j], nameToTypeMap, helperFunctionReturnMap, out)
		case *ast.CompositeLit:
			if len(leftHandSide) > j {
				parseCompositeLit(t, leftHandSide[j], nameToTypeMap, helperFunctionReturnMap, out)
			} else {
				parseCompositeLit(t, "", nameToTypeMap, helperFunctionReturnMap, out)
			}
		case *ast.CallExpr:
			if len(in.Rhs) != len(in.Lhs) {
				var functionName string
				switch funcInfo := t.Fun.(type) {
				case *ast.Ident:
					functionName = funcInfo.Name
				case *ast.SelectorExpr:
					functionName = funcInfo.Sel.Name
				}
				if _, ok := helperFunctionReturnMap[functionName]; ok {
					for i, thing := range leftHandSide {
						nameToTypeMap[thing] = helperFunctionReturnMap[functionName][i]
					}
				}
			}
			parseCallExpr(t, nameToTypeMap, helperFunctionReturnMap, out)
		}
	}
}

func parseHelperFunction(in *ast.FuncDecl, helperFunctionReturnMap map[string][]string, out *set.Set) {
	functionName := in.Name.Name
	if in.Type.Results != nil {
		for _, r := range in.Type.Results.List {
			switch rt := r.Type.(type) {
			case *ast.SelectorExpr:
				if pkg, ok := rt.X.(*ast.Ident); ok {
					pkgName := pkg.Name
					pkgStruct := rt.Sel.Name
					helperFunctionReturnMap[functionName] = append(helperFunctionReturnMap[functionName], fmt.Sprintf("%s.%s", pkgName, pkgStruct))
				}
			case *ast.StarExpr:
				switch x := rt.X.(type) {
				case *ast.Ident:
					helperFunctionReturnMap[functionName] = append(helperFunctionReturnMap[functionName], x.Name)
				case *ast.SelectorExpr:
					if pkg, ok := x.X.(*ast.Ident); ok {
						pkgName := pkg.Name
						pkgStruct := x.Sel.Name
						helperFunctionReturnMap[functionName] = append(helperFunctionReturnMap[functionName], fmt.Sprintf("%s.%s", pkgName, pkgStruct))
					}
				}
			case *ast.Ident:
				helperFunctionReturnMap[functionName] = append(helperFunctionReturnMap[functionName], rt.Name)
			}
		}
	}
}

// parseFuncLit parses a function literal. From the go/ast docs:
// 		A FuncLit node represents a function literal.
// FuncLits have bodies that we basically need to explore the same way that we explore a normal function.
func parseFuncLit(in *ast.FuncLit, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	for _, le := range in.Body.List {
		parseStmt(le, nameToTypeMap, helperFunctionReturnMap, out)
	}
}

func parseReturnStmt(in *ast.ReturnStmt, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	for _, x := range in.Results {
		if y, ok := x.(*ast.CallExpr); ok {
			parseExpr(y.Fun, nameToTypeMap, helperFunctionReturnMap, out)
		}
	}
}

func parseSelectStmt(in *ast.SelectStmt, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	for _, x := range in.Body.List {
		if y, ok := x.(*ast.CommClause); ok {
			for _, z := range y.Body {
				parseStmt(z, nameToTypeMap, helperFunctionReturnMap, out)
			}
		}
	}
}

// parseSendStmt parses a send statement. (<-)
func parseSendStmt(in *ast.SendStmt, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	if n, ok := in.Value.(*ast.CallExpr); ok {
		parseCallExpr(n, nameToTypeMap, helperFunctionReturnMap, out)
	}
}

func parseSwitchStmt(in *ast.SwitchStmt, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	for _, x := range in.Body.List {
		if y, ok := x.(*ast.CaseClause); ok {
			for _, z := range y.Body {
				parseStmt(z, nameToTypeMap, helperFunctionReturnMap, out)
			}
		}
	}
}

// parseTypeSwitchStmt parses
func parseTypeSwitchStmt(in *ast.TypeSwitchStmt, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	if in.Body != nil {
		for _, x := range in.Body.List {
			if y, ok := x.(*ast.CaseClause); ok {
				for _, z := range y.Body {
					parseStmt(z, nameToTypeMap, helperFunctionReturnMap, out)
				}
			}
		}
	}
}

// parseStmt parses a statement. From the go/ast docs:
// 		All statement nodes implement the Stmt interface.
// Cases we don't handle:
//		BadStmt - we only parse valid code
//		BlockStmt (sort of, we iterate over these in the form of `x.Body.List`)
//		these are simply unnecessary:
//			BranchStmt
//			EmptyStmt
//			IncDeclStmt
//			LabeledStmt
func parseStmt(in ast.Stmt, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	switch e := in.(type) {
	case *ast.AssignStmt: // handles things like `e := Example{}` (with or without &)
		parseAssignStmt(e, nameToTypeMap, helperFunctionReturnMap, out)
	// NOTE: even though RangeStmt/IfStmt/ForStmt are handled identically, Go will (rightfully) complain when trying
	// to use a multiple case statement (i.e. `case *ast.RangeStmt, *ast.IfStmt`), so we're doing it this way.
	case *ast.RangeStmt:
		for _, x := range e.Body.List {
			parseStmt(x, nameToTypeMap, helperFunctionReturnMap, out)
		}
	case *ast.IfStmt:
		for _, x := range e.Body.List {
			parseStmt(x, nameToTypeMap, helperFunctionReturnMap, out)
		}
	case *ast.ForStmt:
		for _, x := range e.Body.List {
			parseStmt(x, nameToTypeMap, helperFunctionReturnMap, out)
		}
	case *ast.DeclStmt:
		parseDeclStmt(e, nameToTypeMap)
	case *ast.ExprStmt:
		parseExprStmt(e, nameToTypeMap, helperFunctionReturnMap, out)
	case *ast.DeferStmt:
		parseExpr(e.Call.Fun, nameToTypeMap, helperFunctionReturnMap, out)
	case *ast.GoStmt:
		parseExpr(e.Call.Fun, nameToTypeMap, helperFunctionReturnMap, out)
	case *ast.ReturnStmt:
		parseReturnStmt(e, nameToTypeMap, helperFunctionReturnMap, out)
	case *ast.SelectStmt:
		parseSelectStmt(e, nameToTypeMap, helperFunctionReturnMap, out)
	case *ast.SendStmt:
		parseSendStmt(e, nameToTypeMap, helperFunctionReturnMap, out)
	case *ast.SwitchStmt:
		parseSwitchStmt(e, nameToTypeMap, helperFunctionReturnMap, out)
	case *ast.TypeSwitchStmt:
		parseTypeSwitchStmt(e, nameToTypeMap, helperFunctionReturnMap, out)
	}
}

func getDeclaredNames(in *ast.File, fileset *token.FileSet, declaredFuncDetails map[string]TarpFunc) {
	for _, d := range in.Decls {
		if f, ok := d.(*ast.FuncDecl); ok {
			declPos := fileset.Position(f.Type.Func)
			functionName := parseFuncDecl(f)

			tf := TarpFunc{
				Name:     functionName,
				Filename: declPos.Filename,
				DeclPos:  declPos,
			}

			if f.Body != nil {
				tf.RBracePos = fileset.Position(f.Body.Lbrace)
				tf.LBracePos = fileset.Position(f.Body.Rbrace)
			}
			declaredFuncDetails[functionName] = tf
		}
	}
}

func getCalledNames(in *ast.File, nameToTypeMap map[string]string, helperFunctionReturnMap map[string][]string, out *set.Set) {
	for _, d := range in.Decls {
		switch n := d.(type) {
		case *ast.GenDecl:
			parseGenDecl(n, nameToTypeMap)
		case *ast.FuncDecl:
			if n.Body != nil {
				for _, le := range n.Body.List {
					parseStmt(le, nameToTypeMap, helperFunctionReturnMap, out)
				}
			}
		}
	}
}

func findHelperFuncs(in *ast.File, helperFunctionReturnMap map[string][]string, out *set.Set) {
	for _, d := range in.Decls {
		if n, ok := d.(*ast.FuncDecl); ok {
			functionName := in.Name.Name
			if !strings.HasPrefix(functionName, "Test") {
				parseHelperFunction(n, helperFunctionReturnMap, out)
			}
		}
	}
}

func demonstration() {
	fmt.Println("this function is called by analyze")
}


func analyze(analyzePackage string) TarpReport {
	demonstration()

	gopath := os.Getenv("GOPATH")

	pkgDir := strings.Join([]string{gopath, "src", analyzePackage}, "/")
	if analyzePackage == "." {
		var err error
		pkgDir, err = os.Getwd()
		if err != nil {
			log.Fatalf("error encountered getting current working directory: %v", err)
		}
	}

	if debug {
		log.Printf("package directory: %s", pkgDir)
	}

	_, err := os.Stat(pkgDir)
	if os.IsNotExist(err) {
		log.Fatalf("packageDir doesn't exist: %s", pkgDir)
	}

	astPkg, err := parser.ParseDir(fileset, pkgDir, nil, parser.AllErrors)
	if err != nil {
		log.Fatal(err)
	}

	if len(astPkg) == 0 || astPkg == nil {
		log.Fatal("no go files found!")
	}

	declaredFuncInfo := map[string]TarpFunc{}
	calledFuncs := set.New("init")
	helperFunctionReturnMap := map[string][]string{}
	nameToTypeMap := map[string]string{}

	// find all helper funcs first so we have an idea of what they are.
	for _, pkg := range astPkg {
		for name, f := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				findHelperFuncs(f, helperFunctionReturnMap, calledFuncs)
			}
		}
	}

	for _, pkg := range astPkg {
		for name, f := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				getCalledNames(f, nameToTypeMap, helperFunctionReturnMap, calledFuncs)
			} else {
				getDeclaredNames(f, fileset, declaredFuncInfo)
			}
		}
	}

	declaredFuncs := set.New()
	for _, f := range declaredFuncInfo {
		declaredFuncs.Add(f.Name)
	}

	for _, x := range set.StringSlice(set.Difference(calledFuncs, declaredFuncs)) {
		calledFuncs.Remove(x)
	}

	tr := TarpReport{
		DeclaredDetails: declaredFuncInfo,
		Declared:        declaredFuncs,
		Called:          calledFuncs,
	}
	return tr
}
