package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
)

// loaders are the terminals that load one entity into the tracker of the
// context they run on.
var loaders = map[string]bool{
	"Find":   true,
	"First":  true,
	"Single": true,
	"Last":   true,
}

// load is an entity read through a context.
type load struct {
	entity types.Object
	handle types.Object // the context it was loaded through
	at     token.Pos
}

// save is a SaveChanges call on a context.
type save struct {
	handle types.Object
	call   *ast.CallExpr
}

// sessions holds what checkSessions learns of one function body.
type sessions struct {
	pass *analysis.Pass
	body *ast.BlockStmt
	// assigned counts the assignments to each variable, in closures too, with
	// one whose address is taken counted as given anything.
	assigned map[types.Object]int
	// parent maps a variable set from ant.Session to the variable it was given,
	// or to nil when that was no variable.
	parent  map[types.Object]types.Object
	loads   []load // in the order of the source
	changed map[types.Object]token.Pos // the first change to each entity
	escaped map[types.Object]bool
	saves   []save
}

// checkSessions reports an entity loaded through one context and changed, and
// then a SaveChanges on another context, which saves only what its own tracker
// holds: it returns nil and writes nothing (#153). Most often the entity was
// loaded through the root, which is in scope everywhere, instead of the session.
//
// Only contexts whose trackers surely differ are compared: a session and the
// context it was made from, or two sessions. A transaction's context shares its
// tracker, and two parameters may be one context. Each function body is checked
// on its own, in the order of the source, so a closure's loads are not compared
// with its caller's saves. An entity that goes anywhere, as an argument, a value
// or a result, may be attached or saved there, and is left alone.
func checkSessions(pass *analysis.Pass, in *inspector.Inspector) {
	in.Preorder(
		[]ast.Node{
			(*ast.FuncDecl)(nil),
			(*ast.FuncLit)(nil),
		},
		func(n ast.Node) {
			var body *ast.BlockStmt

			switch n := n.(type) {
			case *ast.FuncDecl:
				body = n.Body
			case *ast.FuncLit:
				body = n.Body
			}

			if body == nil {
				return
			}

			s := &sessions{
				pass:     pass,
				body:     body,
				assigned: map[types.Object]int{},
				parent:   map[types.Object]types.Object{},
				changed:  map[types.Object]token.Pos{},
				escaped:  map[types.Object]bool{},
			}

			s.count()
			s.follow()
			s.report()
		},
	)
}

// count counts the assignments to each variable, and finds the uses of each
// that let it go elsewhere, in the body and the closures in it.
func (s *sessions) count() {
	var stack []ast.Node

	ast.Inspect(s.body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]

			return true
		}

		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				s.assign(lhs)
			}
		case *ast.IncDecStmt:
			s.assign(n.X)
		case *ast.RangeStmt:
			s.assign(n.Key)
			s.assign(n.Value)
		case *ast.ValueSpec:
			for _, name := range n.Names {
				if len(n.Values) > 0 {
					s.assign(name)
				}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				if object := s.variable(n.X); object != nil {
					s.assigned[object] += 2 // anything may be stored through it
				}
			}
		case *ast.Ident:
			if object := s.variable(n); object != nil && !s.stays(n, stack) {
				s.escaped[object] = true
			}
		}

		stack = append(stack, n)

		return true
	})
}

// assign counts an assignment to e, if it is a variable.
func (s *sessions) assign(e ast.Expr) {
	if object := s.variable(e); object != nil {
		s.assigned[object]++
	}
}

// variable returns the variable e names, or nil.
func (s *sessions) variable(e ast.Expr) types.Object {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}

	object, ok := s.pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || object.IsField() {
		return nil
	}

	return object
}

// stays reports whether the use of id, below the nodes on stack, keeps the value
// where it is: a field of it read or written, a comparison, or its own
// assignment.
func (s *sessions) stays(id *ast.Ident, stack []ast.Node) bool {
	if len(stack) == 0 {
		return true
	}

	switch parent := stack[len(stack)-1].(type) {
	case *ast.SelectorExpr:
		return parent.X == id || parent.Sel == id
	case *ast.BinaryExpr:
		return parent.Op == token.EQL || parent.Op == token.NEQ
	case *ast.AssignStmt:
		for _, lhs := range parent.Lhs {
			if lhs == id {
				return true
			}
		}
	case *ast.IncDecStmt, *ast.ValueSpec, *ast.RangeStmt:
		return true
	case *ast.KeyValueExpr:
		return parent.Key == id // a field name in a composite literal
	}

	return false
}

// follow finds the sessions, loads, changes and saves of the body itself, not
// those of the closures in it.
func (s *sessions) follow() {
	ast.Inspect(s.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			s.assignment(n.Lhs, n.Rhs)

			for _, lhs := range n.Lhs {
				s.change(lhs, n.Pos())
			}
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(n.Names))
			for i, name := range n.Names {
				lhs[i] = name
			}

			s.assignment(lhs, n.Values)
		case *ast.IncDecStmt:
			s.change(n.X, n.Pos())
		case *ast.CallExpr:
			if handle := s.saveOn(n); handle != nil {
				s.saves = append(s.saves, save{handle, n})
			}
		}

		return true
	})
}

// assignment records a session made, or an entity loaded, by an assignment.
func (s *sessions) assignment(lhs, rhs []ast.Expr) {
	if len(lhs) == len(rhs) {
		for i := range lhs {
			if call, ok := ast.Unparen(rhs[i]).(*ast.CallExpr); ok {
				s.session(lhs[i], call)
			}
		}
	}

	if len(rhs) != 1 || len(lhs) == 0 {
		return
	}

	call, ok := ast.Unparen(rhs[0]).(*ast.CallExpr)
	if !ok {
		return
	}

	if handle := s.loadedThrough(call); handle != nil {
		if entity := s.variable(lhs[0]); entity != nil {
			s.loads = append(s.loads, load{entity, handle, call.Pos()})
		}
	}
}

// session records lhs as a session when call is ant.Session.
func (s *sessions) session(lhs ast.Expr, call *ast.CallExpr) {
	if !isAntCall(s.pass, call, "Session") || len(call.Args) == 0 {
		return
	}

	if handle := s.variable(lhs); handle != nil {
		s.parent[handle] = s.variable(call.Args[0])
	}
}

// change records a change to a field of an entity: e is a field of it, as
// u.Age, or a field of one, as u.Address.City.
func (s *sessions) change(e ast.Expr, at token.Pos) {
	selector, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return
	}

	for {
		inner, ok := ast.Unparen(selector.X).(*ast.SelectorExpr)
		if !ok {
			break
		}

		selector = inner
	}

	entity := s.variable(selector.X)
	if entity == nil {
		return
	}

	if first, ok := s.changed[entity]; !ok || at < first {
		s.changed[entity] = at
	}
}

// loadedThrough returns the context a call such as db.Users.Find(ctx, id) or
// db.Users.Where(...).First(ctx) loads an entity through, or nil.
func (s *sessions) loadedThrough(call *ast.CallExpr) types.Object {
	selector, ok := ast.Unparen(indexed(call.Fun)).(*ast.SelectorExpr)
	if !ok || !loaders[selector.Sel.Name] {
		return nil
	}

	for e := ast.Expr(selector); ; {
		switch x := ast.Unparen(e).(type) {
		case *ast.CallExpr:
			e = x.Fun
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.SelectorExpr:
			selection := s.pass.TypesInfo.Selections[x]
			if selection == nil {
				return nil
			}

			if selection.Kind() == types.FieldVal {
				if named := antType(selection.Type()); named == nil || named.Obj().Name() != "Table" {
					return nil
				}

				return s.variable(x.X)
			}

			if queryArg(receiverOf(selection)) == nil {
				return nil
			}

			e = x.X
		default:
			return nil
		}
	}
}

// saveOn returns the context of a call h.SaveChanges(ctx), or nil.
func (s *sessions) saveOn(call *ast.CallExpr) types.Object {
	selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "SaveChanges" {
		return nil
	}

	selection := s.pass.TypesInfo.Selections[selector]
	if selection == nil || selection.Kind() != types.MethodVal {
		return nil
	}

	if named := antType(receiverOf(selection)); named == nil || named.Obj().Name() != "Context" {
		return nil
	}

	return s.variable(selector.X)
}

// fixed reports whether a variable holds the value it was given: a local one
// assigned once, or a parameter or outer one never assigned here.
func (s *sessions) fixed(object types.Object) bool {
	if object.Pos() >= s.body.Pos() && object.Pos() < s.body.End() {
		return s.assigned[object] == 1
	}

	return s.assigned[object] == 0
}

// apart reports whether contexts a and b surely have trackers of their own.
func (s *sessions) apart(a, b types.Object) bool {
	if a == b || !s.fixed(a) || !s.fixed(b) {
		return false
	}

	ofA, aSession := s.parent[a]
	ofB, bSession := s.parent[b]

	return aSession && bSession || bSession && ofB == a || aSession && ofA == b
}

// savedLater reports whether a SaveChanges on handle itself comes after at, as
// on a root that tracks.
func (s *sessions) savedLater(handle types.Object, at token.Pos) bool {
	for _, sv := range s.saves {
		if sv.handle == handle && sv.call.Pos() > at {
			return true
		}
	}

	return false
}

// report reports each save that leaves out a changed entity loaded elsewhere,
// naming the first such entity.
func (s *sessions) report() {
	for _, sv := range s.saves {
		for _, l := range s.loads {
			entity := l.entity
			at, changed := s.changed[entity]

			switch {
			case !changed, at < l.at, sv.call.Pos() < at:
				continue
			case s.escaped[entity], !s.fixed(entity), s.savedLater(l.handle, at):
				continue
			case !s.apart(l.handle, sv.handle):
				continue
			}

			s.pass.Reportf(
				sv.call.Pos(),
				"%s was loaded through %s and changed, but %s.SaveChanges saves only what %s tracks: "+
					"load it through %s, or ant.Attach it to %s first",
				entity.Name(),
				l.handle.Name(),
				sv.handle.Name(),
				sv.handle.Name(),
				sv.handle.Name(),
				sv.handle.Name(),
			)

			break
		}
	}
}
