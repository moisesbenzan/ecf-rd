// Package xsd validates DGII XML documents against the official XML Schemas
// published by DGII (e-CF 31-47, RFCE 32, ARECF, ACECF, ANECF, Semilla v1.0).
//
// The schemas are embedded and compiled once. Only the XSD subset used by
// DGII is supported: global element with anonymous complex types made of
// xs:sequence particles (with minOccurs/maxOccurs), a trailing xs:any
// (processContents="skip") and named simple types restricting xs:string,
// xs:integer, xs:decimal or xs:dateTime with the facets enumeration,
// pattern, minLength, maxLength, minInclusive, minExclusive, maxInclusive,
// totalDigits and fractionDigits. Unsupported constructs make compilation
// fail loudly instead of being ignored.
package xsd

import (
	"bytes"
	"embed"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

//go:embed schemas/*.xsd
var schemaFS embed.FS

// Document identifies a DGII document schema.
type Document string

// Schemas available.
const (
	ECF31   Document = "ECF-31"
	ECF32   Document = "ECF-32"
	ECF33   Document = "ECF-33"
	ECF34   Document = "ECF-34"
	ECF41   Document = "ECF-41"
	ECF43   Document = "ECF-43"
	ECF44   Document = "ECF-44"
	ECF45   Document = "ECF-45"
	ECF46   Document = "ECF-46"
	ECF47   Document = "ECF-47"
	RFCE    Document = "RFCE-32"
	ARECF   Document = "ARECF"
	ACECF   Document = "ACECF"
	ANECF   Document = "ANECF"
	Semilla Document = "Semilla"
)

// ForTipo returns the e-CF schema for a receipt type (31..47).
func ForTipo(tipo int) (Document, bool) {
	d := Document(fmt.Sprintf("ECF-%d", tipo))
	_, err := schemaFS.Open("schemas/" + string(d) + ".xsd")
	return d, err == nil
}

// Error is a schema violation.
type Error struct {
	Path    string `json:"ruta"`
	Message string `json:"mensaje"`
}

func (e Error) Error() string { return e.Path + ": " + e.Message }

// Errors is a list of schema violations.
type Errors []Error

func (es Errors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

// Options tune validation.
type Options struct {
	// AllowMissingAny accepts documents without the trailing wildcard
	// element (the signature), to validate documents before signing.
	AllowMissingAny bool
	// MaxErrors stops after this many errors (0 = 50).
	MaxErrors int
}

// Validate validates data against the schema of doc.
func Validate(doc Document, data []byte, opt Options) (Errors, error) {
	s, err := load(doc)
	if err != nil {
		return nil, err
	}
	root, err := parseInstance(data)
	if err != nil {
		return Errors{{Path: "/", Message: "XML mal formado: " + err.Error()}}, nil
	}
	if opt.MaxErrors <= 0 {
		opt.MaxErrors = 50
	}
	v := &validator{opt: opt}
	if root.name != s.root.name {
		v.add("/"+root.name, fmt.Sprintf("se esperaba el elemento raíz <%s>", s.root.name))
		return v.errs, nil
	}
	v.element(s.root, root, "/"+root.name)
	return v.errs, nil
}

// Enumeration returns the enumerated values of a named simple type in doc
// (e.g. the province codes). It is used to expose the official code tables.
func Enumeration(doc Document, typeName string) ([]string, error) {
	s, err := load(doc)
	if err != nil {
		return nil, err
	}
	st, ok := s.types[typeName]
	if !ok {
		return nil, fmt.Errorf("xsd: tipo %q no existe en %s", typeName, doc)
	}
	return st.enumRaw, nil
}

// ---- schema model ----

type schema struct {
	root  *elementDecl
	types map[string]*simpleType
}

type elementDecl struct {
	name     string
	min, max int // max -1 = unbounded
	any      bool
	typeName string
	simple   *simpleType
	children []*elementDecl // complex content (sequence)
	complex  bool
}

type simpleType struct {
	name           string
	base           string
	enumRaw        []string
	enum           map[string]bool
	patterns       []*regexp.Regexp
	patternSrc     []string
	minLen, maxLen int // -1 = unset
	minIncl        *big.Rat
	minExcl        *big.Rat
	maxIncl        *big.Rat
	totalDigits    int
	fractionDigits int // -1 = unset
}

var (
	cacheMu sync.Mutex
	cache   = map[Document]*schema{}
)

func load(doc Document) (*schema, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if s, ok := cache[doc]; ok {
		return s, nil
	}
	data, err := schemaFS.ReadFile("schemas/" + string(doc) + ".xsd")
	if err != nil {
		return nil, fmt.Errorf("xsd: esquema %q no disponible", doc)
	}
	s, err := compile(data)
	if err != nil {
		return nil, fmt.Errorf("xsd: compilando %s: %w", doc, err)
	}
	cache[doc] = s
	return s, nil
}

// MustLoadAll compiles every embedded schema; used at startup and in tests.
func MustLoadAll() {
	for _, d := range []Document{ECF31, ECF32, ECF33, ECF34, ECF41, ECF43, ECF44, ECF45, ECF46, ECF47, RFCE, ARECF, ACECF, ANECF, Semilla} {
		if _, err := load(d); err != nil {
			panic(err)
		}
	}
}

// --- compilation from the XSD DOM ---

type node struct {
	name     string // local name
	attrs    map[string]string
	children []*node
	text     strings.Builder
}

func parseTree(r io.Reader, withText bool) (*node, error) {
	dec := xml.NewDecoder(r)
	var stack []*node
	var root *node
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				// NCName/QName attributes are whitespace-collapsed by schema
				// processors (ECF-31 declares name=" IndicadorServicio...").
				n.attrs[a.Name.Local] = strings.TrimSpace(a.Value)
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			} else if root == nil {
				root = n
			} else {
				return nil, fmt.Errorf("más de un elemento raíz")
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if withText && len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("documento vacío")
	}
	return root, nil
}

func compile(data []byte) (*schema, error) {
	tree, err := parseTree(bytes.NewReader(data), false)
	if err != nil {
		return nil, err
	}
	if tree.name != "schema" {
		return nil, fmt.Errorf("no es un xs:schema")
	}
	s := &schema{types: map[string]*simpleType{}}
	var elems []*node
	for _, c := range tree.children {
		switch c.name {
		case "simpleType":
			st, err := compileSimple(c)
			if err != nil {
				return nil, err
			}
			s.types[st.name] = st
		case "element":
			elems = append(elems, c)
		default:
			return nil, fmt.Errorf("construcción global no soportada: %s", c.name)
		}
	}
	if len(elems) != 1 {
		return nil, fmt.Errorf("se esperaba un único elemento global, hay %d", len(elems))
	}
	root, err := s.compileElement(elems[0])
	if err != nil {
		return nil, err
	}
	s.root = root
	return s, nil
}

func occurs(v string, def int) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return def, nil
	}
	if v == "unbounded" {
		return -1, nil
	}
	return strconv.Atoi(v)
}

func (s *schema) compileElement(n *node) (*elementDecl, error) {
	e := &elementDecl{name: n.attrs["name"]}
	var err error
	if e.min, err = occurs(n.attrs["minOccurs"], 1); err != nil {
		return nil, err
	}
	if e.max, err = occurs(n.attrs["maxOccurs"], 1); err != nil {
		return nil, err
	}
	if t := n.attrs["type"]; t != "" {
		e.typeName = t
		if strings.HasPrefix(t, "xs:") {
			e.simple = &simpleType{name: t, base: t, minLen: -1, maxLen: -1, fractionDigits: -1}
		} else {
			st, ok := s.types[t]
			if !ok {
				return nil, fmt.Errorf("elemento %s: tipo %q no definido", e.name, t)
			}
			e.simple = st
		}
	}
	for _, c := range n.children {
		switch c.name {
		case "complexType":
			e.complex = true
			for _, cc := range c.children {
				if cc.name != "sequence" {
					return nil, fmt.Errorf("elemento %s: %s no soportado", e.name, cc.name)
				}
				for _, p := range cc.children {
					switch p.name {
					case "element":
						ce, err := s.compileElement(p)
						if err != nil {
							return nil, err
						}
						e.children = append(e.children, ce)
					case "any":
						a := &elementDecl{any: true, name: "*"}
						if a.min, err = occurs(p.attrs["minOccurs"], 1); err != nil {
							return nil, err
						}
						if a.max, err = occurs(p.attrs["maxOccurs"], 1); err != nil {
							return nil, err
						}
						e.children = append(e.children, a)
					default:
						return nil, fmt.Errorf("partícula %s no soportada", p.name)
					}
				}
			}
		case "simpleType":
			st, err := compileSimple(c)
			if err != nil {
				return nil, err
			}
			e.simple = st
		case "annotation":
		default:
			return nil, fmt.Errorf("elemento %s: hijo %s no soportado", e.name, c.name)
		}
	}
	return e, nil
}

func compileSimple(n *node) (*simpleType, error) {
	st := &simpleType{name: n.attrs["name"], minLen: -1, maxLen: -1, fractionDigits: -1, enum: map[string]bool{}}
	var restr *node
	for _, c := range n.children {
		if c.name == "restriction" {
			restr = c
		}
	}
	if restr == nil {
		return nil, fmt.Errorf("tipo %s sin restriction", st.name)
	}
	st.base = restr.attrs["base"]
	switch st.base {
	case "xs:string", "xs:integer", "xs:decimal", "xs:dateTime":
	default:
		return nil, fmt.Errorf("tipo %s: base %s no soportada", st.name, st.base)
	}
	for _, f := range restr.children {
		v := f.attrs["value"]
		var err error
		switch f.name {
		case "enumeration":
			st.enumRaw = append(st.enumRaw, v)
			st.enum[st.canonical(v)] = true
		case "pattern":
			re, rerr := translatePattern(v)
			if rerr != nil {
				return nil, fmt.Errorf("tipo %s: patrón %q: %w", st.name, v, rerr)
			}
			st.patterns = append(st.patterns, re)
			st.patternSrc = append(st.patternSrc, v)
		case "minLength":
			st.minLen, err = strconv.Atoi(v)
		case "maxLength":
			st.maxLen, err = strconv.Atoi(v)
		case "length":
			st.minLen, err = strconv.Atoi(v)
			st.maxLen = st.minLen
		case "minInclusive":
			st.minIncl, err = rat(v)
		case "minExclusive":
			st.minExcl, err = rat(v)
		case "maxInclusive":
			st.maxIncl, err = rat(v)
		case "totalDigits":
			st.totalDigits, err = strconv.Atoi(v)
		case "fractionDigits":
			st.fractionDigits, err = strconv.Atoi(v)
		case "annotation", "whiteSpace":
		default:
			return nil, fmt.Errorf("tipo %s: faceta %s no soportada", st.name, f.name)
		}
		if err != nil {
			return nil, fmt.Errorf("tipo %s: faceta %s: %w", st.name, f.name, err)
		}
	}
	if len(st.enum) == 0 {
		st.enum = nil
	}
	return st, nil
}

func rat(v string) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(v))
	if !ok {
		return nil, fmt.Errorf("valor numérico inválido %q", v)
	}
	return r, nil
}

// canonical returns the value-space key used for enumeration comparison.
func (st *simpleType) canonical(v string) string {
	switch st.base {
	case "xs:integer", "xs:decimal":
		if r, ok := new(big.Rat).SetString(strings.TrimSpace(v)); ok {
			return r.RatString()
		}
	}
	return v
}

// translatePattern converts an XSD regular expression to an anchored Go
// regexp. XSD regexes are implicitly anchored and use Unicode-aware class
// escapes; "^" and "$" have no anchor meaning in XSD but some DGII patterns
// use them as anchors, so leading/trailing ones are dropped.
func translatePattern(p string) (*regexp.Regexp, error) {
	p = strings.TrimPrefix(p, "^")
	if strings.HasSuffix(p, "$") && !strings.HasSuffix(p, `\$`) {
		p = strings.TrimSuffix(p, "$")
	}
	const (
		digit = `\p{Nd}`
		space = `\x20\t\n\r`
		word  = `\p{L}\p{M}\p{N}\p{S}\p{Pc}`
	)
	var b strings.Builder
	inClass := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			e := p[i+1]
			i++
			switch e {
			case 'd':
				if inClass {
					b.WriteString(digit)
				} else {
					b.WriteString("[" + digit + "]")
				}
			case 'D':
				if inClass {
					return nil, fmt.Errorf(`\D dentro de clase no soportado`)
				}
				b.WriteString("[^" + digit + "]")
			case 's':
				if inClass {
					b.WriteString(space)
				} else {
					b.WriteString("[" + space + "]")
				}
			case 'S':
				if inClass {
					return nil, fmt.Errorf(`\S dentro de clase no soportado`)
				}
				b.WriteString("[^" + space + "]")
			case 'w':
				if inClass {
					b.WriteString(word)
				} else {
					b.WriteString("[" + word + "]")
				}
			case 'W':
				if inClass {
					return nil, fmt.Errorf(`\W dentro de clase no soportado`)
				}
				b.WriteString("[^" + word + "]")
			default:
				b.WriteByte('\\')
				b.WriteByte(e)
			}
			continue
		}
		switch c {
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '$':
			if inClass {
				b.WriteByte(c)
			} else {
				b.WriteString(`\$`)
			}
			continue
		case '^':
			// Only a negation right after "[" keeps its special meaning.
			if !(inClass && i > 0 && p[i-1] == '[') {
				b.WriteString(`\^`)
				continue
			}
		}
		b.WriteByte(c)
	}
	return regexp.Compile(`^(?:` + b.String() + `)$`)
}

// ---- instance validation ----

func parseInstance(data []byte) (*node, error) {
	return parseTree(bytes.NewReader(data), true)
}

type validator struct {
	opt  Options
	errs Errors
}

func (v *validator) add(path, msg string) {
	if len(v.errs) < v.opt.MaxErrors {
		v.errs = append(v.errs, Error{Path: path, Message: msg})
	}
}

func (v *validator) element(d *elementDecl, n *node, path string) {
	if d.complex {
		if strings.TrimSpace(n.text.String()) != "" {
			v.add(path, "no admite texto")
		}
		v.sequence(d, n.children, path)
		return
	}
	if len(n.children) > 0 {
		v.add(path, "no admite elementos hijos")
		return
	}
	if d.simple != nil {
		if msg := d.simple.check(n.text.String()); msg != "" {
			v.add(path, msg)
		}
	}
}

func (v *validator) sequence(d *elementDecl, kids []*node, path string) {
	i := 0
	counts := map[string]int{}
	reported := map[int]bool{}
	for _, p := range d.children {
		// Skip (and report once) elements the type does not declare at all,
		// so that one stray element does not cascade into ordering errors.
		for !p.any && i < len(kids) && !declared(d, kids[i].name) {
			v.add(path+"/"+kids[i].name, "elemento no permitido en este tipo de documento")
			reported[i] = true
			i++
		}
		count := 0
		for i < len(kids) && (p.max < 0 || count < p.max) && (p.any || kids[i].name == p.name) {
			k := kids[i]
			if !p.any {
				counts[k.name]++
				idx := ""
				if p.max != 1 {
					idx = fmt.Sprintf("[%d]", counts[k.name])
				}
				v.element(p, k, path+"/"+k.name+idx)
			}
			count++
			i++
		}
		if count < p.min {
			if p.any {
				if !v.opt.AllowMissingAny {
					v.add(path, "falta el elemento final (firma digital)")
				}
			} else {
				v.add(path+"/"+p.name, "elemento obligatorio ausente")
			}
		}
	}
	for ; i < len(kids); i++ {
		if reported[i] {
			continue
		}
		name := kids[i].name
		if declared(d, name) {
			v.add(path+"/"+name, "elemento fuera de orden o repetido más de lo permitido")
		} else {
			v.add(path+"/"+name, "elemento no permitido en este tipo de documento")
		}
	}
}

func declared(d *elementDecl, name string) bool {
	for _, c := range d.children {
		if c.name == name {
			return true
		}
	}
	return false
}

// check validates a lexical value; it returns "" when valid.
func (st *simpleType) check(raw string) string {
	v := raw
	switch st.base {
	case "xs:integer", "xs:decimal", "xs:dateTime":
		v = strings.TrimSpace(raw)
	}
	switch st.base {
	case "xs:integer":
		if !isInteger(v) {
			return fmt.Sprintf("%q no es un entero válido", v)
		}
	case "xs:decimal":
		if !isDecimal(v) {
			return fmt.Sprintf("%q no es un decimal válido", v)
		}
	}
	if st.enum != nil && !st.enum[st.canonical(v)] {
		if len(st.enumRaw) <= 12 {
			return fmt.Sprintf("valor %q no permitido (valores: %s)", v, strings.Join(st.enumRaw, ", "))
		}
		return fmt.Sprintf("valor %q no está en la tabla de códigos (%s)", v, st.name)
	}
	if len(st.patterns) > 0 {
		ok := false
		for _, re := range st.patterns {
			if re.MatchString(v) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Sprintf("valor %q no cumple el formato %s", v, st.patternSrc[0])
		}
	}
	n := utf8.RuneCountInString(v)
	if st.minLen >= 0 && n < st.minLen {
		return fmt.Sprintf("largo mínimo %d", st.minLen)
	}
	if st.maxLen >= 0 && n > st.maxLen {
		return fmt.Sprintf("largo máximo %d", st.maxLen)
	}
	if st.base == "xs:integer" || st.base == "xs:decimal" {
		r, ok := new(big.Rat).SetString(v)
		if !ok {
			return fmt.Sprintf("%q no es numérico", v)
		}
		if st.minIncl != nil && r.Cmp(st.minIncl) < 0 {
			return fmt.Sprintf("debe ser ≥ %s", st.minIncl.FloatString(2))
		}
		if st.minExcl != nil && r.Cmp(st.minExcl) <= 0 {
			return fmt.Sprintf("debe ser > %s", st.minExcl.FloatString(2))
		}
		if st.maxIncl != nil && r.Cmp(st.maxIncl) > 0 {
			return fmt.Sprintf("debe ser ≤ %s", st.maxIncl.FloatString(2))
		}
		intDigits, fracDigits := digits(v)
		if st.fractionDigits >= 0 && fracDigits > st.fractionDigits {
			return fmt.Sprintf("admite un máximo de %d decimales", st.fractionDigits)
		}
		if st.totalDigits > 0 && intDigits+fracDigits > st.totalDigits {
			return fmt.Sprintf("admite un máximo de %d dígitos", st.totalDigits)
		}
	}
	return ""
}

func isInteger(v string) bool {
	if v == "" {
		return false
	}
	if v[0] == '+' || v[0] == '-' {
		v = v[1:]
	}
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}

func isDecimal(v string) bool {
	if v == "" {
		return false
	}
	if v[0] == '+' || v[0] == '-' {
		v = v[1:]
	}
	seenDigit, seenDot := false, false
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c >= '0' && c <= '9':
			seenDigit = true
		case c == '.' && !seenDot:
			seenDot = true
		default:
			return false
		}
	}
	return seenDigit
}

// digits returns significant integer and fraction digit counts as defined by
// the totalDigits/fractionDigits facets (leading integer zeros and trailing
// fraction zeros are not significant).
func digits(v string) (int, int) {
	v = strings.TrimLeft(v, "+-")
	ip, fp, _ := strings.Cut(v, ".")
	ip = strings.TrimLeft(ip, "0")
	fp = strings.TrimRight(fp, "0")
	return len(ip), len(fp)
}
