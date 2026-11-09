// Package elfpatch applies declarative edits to ARM32 Thumb ELF binaries,
// locating code by stable properties and placing compiled C stubs in free
// space, so one patch can carry across firmware builds.
package elfpatch

import (
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
)

// PatchSet is the declarative list of edits for one ELF binary.
type PatchSet struct {
	Objects map[string]string `yaml:"objects,omitempty"` // logical name -> stub .o path
	Caves   []CaveSpec        `yaml:"caves,omitempty"`
	Pokes   []Poke            `yaml:"pokes,omitempty"` // shorthand for action: poke edits
	Edits   []Edit            `yaml:"edits,omitempty"`
}

// Poke overwrites bytes; wildcard nibbles in Write keep the original.
type Poke struct {
	Name   string   `yaml:"name,omitempty"`
	Anchor Anchor   `yaml:"anchor"`
	At     *Locator `yaml:"at,omitempty"`
	Write  string   `yaml:"write"`
}

// CaveSpec declares one source of free space. Exactly one field is set.
type CaveSpec struct {
	Reclaim *Anchor    `yaml:"reclaim,omitempty"`
	Range   *VARange   `yaml:"range,omitempty"`
	Slack   *SlackSpec `yaml:"slack,omitempty"`
	Auto    *AutoSpec  `yaml:"auto,omitempty"`
}

// SlackSpec grows the executable segment over its trailing padding, yielding at
// least Min bytes.
type SlackSpec struct {
	Min uint32 `yaml:"min,omitempty"`
}

// AutoSpec lets the patch create space when the declared caves are exhausted,
// first from the executable segment's padding and then by growing it.
type AutoSpec struct{}

// VARange is the virtual-address span [Start,End).
type VARange struct {
	Start  uint32 `yaml:"start"`
	End    uint32 `yaml:"end"`
	Expect string `yaml:"expect"` // masked pattern required at Start
}

// Locator refines an anchor to an exact point.
type Locator struct {
	Pattern    string `yaml:"pattern,omitempty"`
	Offset     *int   `yaml:"offset,omitempty"`
	Occurrence int    `yaml:"occurrence,omitempty"` // 1-based; 0 requires uniqueness
}

// Edit is one modification. Action is poke, nop, detour, call, hook or place.
//
// detour, call and hook displace whole instructions at the site; the stub must
// reproduce any it still needs. A detour jumps to the stub, a call replaces the
// site with a BL, and a hook goes through generated glue. place writes a stub
// over the anchored function.
//
// Nothing checks register liveness. A call clobbers lr, r0-r3, ip and the flags
// and a lean hook clobbers r0-r3 and the flags, so the site must not need them.
// A detour keeps the host's lr, but its stub must leave through _ret or return
// on the host's behalf.
type Edit struct {
	Name   string   `yaml:"name,omitempty"`
	Anchor Anchor   `yaml:"anchor"`
	At     *Locator `yaml:"at,omitempty"`
	Action string   `yaml:"action"`

	Write string `yaml:"write,omitempty"` // poke
	Count int    `yaml:"count,omitempty"` // nop: instruction count, default 1

	// Cover is the minimum bytes displaced (default: the At pattern, at least
	// 4). For place it is the replaced function's extent: it bounds the stub and
	// the rest is zeroed.
	Cover int `yaml:"cover,omitempty"`

	Object string `yaml:"object,omitempty"`
	Entry  string `yaml:"entry,omitempty"`

	// Ctx selects a hook receiving the full register frame, able to choose among
	// the primary return (index 0) and Returns by its return value. Otherwise
	// the hook is lean: Args are passed in r0-r3 and the result lands in Result
	// (default r0), clobbering r0-r3 and the flags.
	Ctx     bool     `yaml:"ctx,omitempty"`
	Returns []ExtRef `yaml:"returns,omitempty"`
	Args    []string `yaml:"args,omitempty"`
	Result  string   `yaml:"result,omitempty"`

	// Externals resolves stub imports beyond the host PLT, which is searched
	// automatically. _site and _ret are always defined.
	Externals map[string]ExtRef `yaml:"externals,omitempty"`
}

// ExtRef resolves one external symbol. Exactly one field is set.
type ExtRef struct {
	Import string  `yaml:"import,omitempty"`  // host PLT import
	Export string  `yaml:"export,omitempty"`  // host exported function
	String string  `yaml:"string,omitempty"`  // host string literal
	VA     *uint32 `yaml:"va,omitempty"`      // fixed address; bit 0 selects Thumb
	Expect string  `yaml:"expect,omitempty"`  // masked pattern required there; mandatory for a Thumb va
	UsedBy *Anchor `yaml:"used_by,omitempty"` // function that must use this data va; mandatory for one
}

// ObjectOpener loads a stub object by its path.
type ObjectOpener func(path string) ([]byte, error)

// Apply returns data with the patch set applied. Every edit is resolved against
// the original code and data before any of it is written, so order cannot
// affect anchors, and any failure produces no output. Growing a cave may move
// file offsets while planning, but never addresses.
func (ps *PatchSet) Apply(data []byte, open ObjectOpener) ([]byte, error) {
	if err := ps.Validate(); err != nil {
		return nil, err
	}
	im, err := Open(data)
	if err != nil {
		return nil, err
	}
	p := &planner{ps: ps, im: im, arena: NewArena(im), open: open}
	for i, c := range ps.Caves {
		if err := p.addCave(c); err != nil {
			return nil, fmt.Errorf("cave #%d: %w", i+1, err)
		}
	}
	for i, poke := range ps.Pokes {
		p.label = fmt.Sprintf("poke #%d %q", i+1, poke.Name)
		e := Edit{Name: poke.Name, Anchor: poke.Anchor, At: poke.At, Action: "poke", Write: poke.Write}
		if err := p.plan(&e); err != nil {
			return nil, fmt.Errorf("%s: %w", p.label, err)
		}
	}
	for i := range ps.Edits {
		p.label = fmt.Sprintf("edit #%d %q", i+1, ps.Edits[i].Name)
		if err := p.plan(&ps.Edits[i]); err != nil {
			return nil, fmt.Errorf("%s: %w", p.label, err)
		}
	}
	if err := p.commit(); err != nil {
		return nil, err
	}
	return im.Bytes(), nil
}

// planner accumulates the writes for a patch set.
type planner struct {
	ps     *PatchSet
	im     *Image
	arena  *Arena
	open   ObjectOpener
	label  string
	writes []plannedWrite
}

type plannedWrite struct {
	va    uint32
	data  []byte
	label string
}

func (p *planner) write(va uint32, data []byte) {
	p.writes = append(p.writes, plannedWrite{va, data, p.label})
}

func (p *planner) addCave(c CaveSpec) error {
	if countTrue(c.Reclaim != nil, c.Range != nil, c.Slack != nil, c.Auto != nil) != 1 {
		return fmt.Errorf("exactly one of reclaim, range, slack or auto must be set")
	}
	switch {
	case c.Reclaim != nil:
		return p.arena.Reclaim(*c.Reclaim)
	case c.Range != nil:
		return p.arena.Range(c.Range.Start, c.Range.End, c.Range.Expect)
	case c.Auto != nil:
		p.arena.Auto()
		return nil
	default:
		return p.arena.Slack(c.Slack.Min)
	}
}

// commit clears the unused cave bytes and performs every write, refusing
// overlaps, including an edit landing inside a cave.
func (p *planner) commit() error {
	p.label = "cave fill"
	for _, r := range p.arena.unused() {
		p.write(r.start, make([]byte, r.end-r.start))
	}
	sort.SliceStable(p.writes, func(i, j int) bool { return p.writes[i].va < p.writes[j].va })
	for i := 1; i < len(p.writes); i++ {
		a, b := p.writes[i-1], p.writes[i]
		if uint64(a.va)+uint64(len(a.data)) > uint64(b.va) {
			return fmt.Errorf("%s (%#x, %d bytes) overlaps %s (%#x)", a.label, a.va, len(a.data), b.label, b.va)
		}
	}
	for _, w := range p.writes {
		if err := p.im.WriteVA(w.va, w.data); err != nil {
			return fmt.Errorf("%s: %w", w.label, err)
		}
	}
	return nil
}

func (p *planner) plan(e *Edit) error {
	site, err := e.site(p.im)
	if err != nil {
		return err
	}
	switch e.Action {
	case "poke":
		return p.poke(site, e)
	case "nop":
		return p.nop(site, e)
	case "detour", "call", "hook":
		return p.install(site, e)
	case "place":
		return p.place(site, e)
	default:
		return fmt.Errorf("unknown action %q", e.Action)
	}
}

func (e *Edit) site(im *Image) (uint32, error) {
	base, err := e.Anchor.Resolve(im)
	if err != nil || e.At == nil {
		return base, err
	}
	if e.At.Offset != nil {
		if e.At.Pattern != "" {
			return 0, fmt.Errorf("at takes pattern or offset, not both")
		}
		return base + uint32(*e.At.Offset), nil
	}
	if e.Anchor.isData() {
		return 0, fmt.Errorf("at.pattern needs a code anchor; use at.offset")
	}
	if e.At.Pattern == "" {
		return 0, fmt.Errorf("at needs pattern or offset")
	}
	pat, err := ParsePattern(e.At.Pattern)
	if err != nil {
		return 0, err
	}
	return im.Within(base, pat, e.At.Occurrence)
}

// checkPinned requires a va anchor's expect to cover the n bytes an edit replaces.
func (p *planner) checkPinned(e *Edit, n uint32) error {
	if e.Anchor.VA == nil {
		return nil
	}
	pat, err := parseExpect(e.Anchor.Expect)
	if err != nil {
		return err
	}
	if uint32(pat.Len()) < n {
		return fmt.Errorf("expect covers %d bytes, the edit replaces %d", pat.Len(), n)
	}
	return nil
}

func (p *planner) poke(va uint32, e *Edit) error {
	pat, err := ParsePattern(e.Write)
	if err != nil {
		return err
	}
	if err := p.checkPinned(e, uint32(pat.Len())); err != nil {
		return err
	}
	orig := p.im.ReadVA(va, pat.Len())
	if orig == nil {
		return fmt.Errorf("poke at %#x is not file-backed", va)
	}
	p.write(va, pat.apply(orig))
	return nil
}

func (p *planner) nop(va uint32, e *Edit) error {
	end := va
	for range max(e.Count, 1) {
		k, ok := p.im.insnAt(end)
		if !ok {
			return fmt.Errorf("cannot decode instruction at %#x", end)
		}
		end += uint32(k.Len)
	}
	if err := p.checkPinned(e, end-va); err != nil {
		return err
	}
	p.write(va, nops(end-va))
	return nil
}

func nops(n uint32) []byte {
	return slices.Repeat([]byte{0x00, 0xBF}, int(n/2))
}

// displaced returns how many bytes of whole instructions the site gives up.
func (p *planner) displaced(site uint32, e *Edit) (uint32, error) {
	if site&1 != 0 {
		return 0, fmt.Errorf("site %#x is not on a Thumb instruction boundary", site)
	}
	want := uint32(max(e.Cover, 4))
	if e.Cover == 0 && e.At != nil && e.At.Pattern != "" {
		pat, err := ParsePattern(e.At.Pattern)
		if err != nil {
			return 0, err
		}
		want = max(want, uint32(pat.Len()))
	}
	if p.im.insideIT(site) {
		return 0, fmt.Errorf("site %#x is inside an IT block", site)
	}
	var n uint32
	for n < want {
		va := site + n
		k, ok := p.im.insnAt(va)
		switch {
		case !ok:
			return 0, fmt.Errorf("cannot decode instruction at %#x", va)
		case k.ITCount > 0:
			return 0, fmt.Errorf("overwrite would split an IT block at %#x", va)
		case k.PCRel:
			return 0, fmt.Errorf("overwrite would displace a PC-relative instruction at %#x", va)
		case k.Terminal && n+uint32(k.Len) < want:
			return 0, fmt.Errorf("overwrite would run past the end of the code at %#x", va)
		case p.im.isLiteral(va):
			return 0, fmt.Errorf("overwrite would displace literal data at %#x", va)
		}
		n += uint32(k.Len)
	}
	if from, found := p.im.branchesInto(site, site+n); found {
		return 0, fmt.Errorf("branch at %#x targets the displaced bytes at %#x", from, site)
	}
	if e.Anchor.VA != nil {
		// The stub reproduces what it displaces, so all of it must be pinned.
		pat, err := parseExpect(e.Anchor.Expect)
		if err != nil {
			return 0, err
		}
		if uint32(pat.Len()) < n {
			return 0, fmt.Errorf("expect covers %d bytes, the edit displaces %d", pat.Len(), n)
		}
	}
	return n, nil
}

func (p *planner) loadObject(e *Edit) (*Object, error) {
	path, ok := p.ps.Objects[e.Object]
	if !ok {
		return nil, fmt.Errorf("object %q not declared", e.Object)
	}
	data, err := p.open(path)
	if err != nil {
		return nil, fmt.Errorf("open object %q: %w", path, err)
	}
	obj, err := LoadObject(data, e.Entry)
	if err != nil {
		return nil, fmt.Errorf("object %q: %w", path, err)
	}
	return obj, nil
}

// install places the edit's stub in a cave and redirects the site to it.
func (p *planner) install(site uint32, e *Edit) error {
	if len(e.Returns) > 0 && (e.Action != "hook" || !e.Ctx) {
		return fmt.Errorf("returns needs action: hook with ctx: true")
	}
	covered, err := p.displaced(site, e)
	if err != nil {
		return err
	}
	ret := site + covered
	resolve, err := p.resolver(e, site, ret)
	if err != nil {
		return err
	}
	obj, err := p.loadObject(e)
	if err != nil {
		return err
	}
	base, err := p.arena.Alloc(obj.Size, max(obj.Align, 4))
	if err != nil {
		return err
	}
	code, entry, err := obj.Link(base, resolve)
	if err != nil {
		return err
	}
	p.write(base, code)

	target := entry &^ 1
	if e.Action == "hook" {
		if target, err = p.placeGlue(e, entry, ret); err != nil {
			return err
		}
	}
	top := uint16(topBW)
	if e.Action == "call" {
		top = topBL
	}
	h1, h2, ok := encodeBranch(site, target, top)
	if !ok {
		return fmt.Errorf("target %#x is out of branch range of %#x", target, site)
	}
	jump := binary.LittleEndian.AppendUint16(binary.LittleEndian.AppendUint16(nil, h1), h2)
	p.write(site, append(jump, nops(covered-4)...))
	return nil
}

func (p *planner) placeGlue(e *Edit, entry, ret uint32) (uint32, error) {
	g, err := p.buildGlue(e, entry, ret)
	if err != nil {
		return 0, err
	}
	va, err := p.arena.Alloc(uint32(len(g.code)), g.align())
	if err != nil {
		return 0, err
	}
	code, err := g.link(va)
	if err != nil {
		return 0, err
	}
	p.write(va, code)
	return va, nil
}

func (p *planner) buildGlue(e *Edit, entry, ret uint32) (*thumbAsm, error) {
	if e.Ctx {
		targets := []uint32{ret | 1}
		for i, r := range e.Returns {
			t, err := p.resolveRef(r)
			if err != nil {
				return nil, fmt.Errorf("return #%d: %w", i+1, err)
			}
			if r.Import == "" && t&1 == 0 {
				return nil, fmt.Errorf("return #%d: target %#x is not Thumb code", i+1, t)
			}
			if r.VA != nil {
				if err := p.im.checkReturn(ret-2, t); err != nil {
					return nil, fmt.Errorf("return #%d: %w", i+1, err)
				}
			}
			targets = append(targets, t)
		}
		return ctxGlue(entry, targets), nil
	}
	args := make([]int, len(e.Args))
	for i, a := range e.Args {
		n, err := regNum(a)
		if err != nil {
			return nil, err
		}
		args[i] = n
	}
	result := 0
	if e.Result != "" {
		var err error
		if result, err = regNum(e.Result); err != nil {
			return nil, err
		}
		if result == 13 {
			return nil, fmt.Errorf("result cannot be sp")
		}
	}
	return leanGlue(entry, args, result, ret|1)
}

// place overwrites the anchored function, keeping its address as the entry.
func (p *planner) place(va uint32, e *Edit) error {
	resolve, err := p.resolver(e, va, va)
	if err != nil {
		return err
	}
	obj, err := p.loadObject(e)
	if err != nil {
		return err
	}
	code, entry, err := obj.Link(va, resolve)
	if err != nil {
		return err
	}
	if err := p.im.checkFunction(va); err != nil {
		return err
	}
	if entry&^1 != va {
		return fmt.Errorf("entry is at %#x, must be at the anchored address %#x", entry&^1, va)
	}
	limit := uint32(e.Cover)
	if limit == 0 {
		limit = max(p.im.FuncEnd(va), va) - va
	}
	if uint32(len(code)) > limit {
		return fmt.Errorf("object is %d bytes, exceeds the %d available at %#x", len(code), limit, va)
	}
	if e.Cover > 0 { // the author declared the extent, so blank what the stub leaves
		code = append(code, make([]byte, limit-uint32(len(code)))...)
	}
	p.write(va, code)
	return nil
}

func (p *planner) resolveRef(r ExtRef) (uint32, error) {
	if n := countTrue(r.Import != "", r.Export != "", r.String != "", r.VA != nil); n != 1 {
		return 0, fmt.Errorf("reference needs exactly one selector, has %d", n)
	}
	switch {
	case r.Import != "":
		stub, ok := p.im.PLTStub(r.Import)
		if !ok {
			return 0, fmt.Errorf("import %q has no PLT entry in host", r.Import)
		}
		return stub, nil
	case r.Export != "":
		v, ok := p.im.exports[r.Export]
		if !ok {
			return 0, fmt.Errorf("export %q not found in host", r.Export)
		}
		return v, nil
	case r.String != "":
		return single(fmt.Sprintf("host string %q", r.String), p.im.FindString(r.String))
	default:
		return *r.VA, p.im.checkExpect(*r.VA&^1, r.Expect)
	}
}

// resolver returns the symbol lookup for an edit's stub.
func (p *planner) resolver(e *Edit, site, ret uint32) (func(string) (uint32, bool), error) {
	ext := map[string]uint32{"_site": site | 1, "_ret": ret | 1}
	for name, r := range e.Externals {
		v, err := p.resolveRef(r)
		if err == nil && r.VA != nil {
			err = p.im.checkRawRef(r)
		}
		if err != nil {
			return nil, fmt.Errorf("external %q: %w", name, err)
		}
		ext[name] = v
	}
	return func(name string) (uint32, bool) {
		if v, ok := ext[name]; ok {
			return v, true
		}
		return p.im.PLTStub(name)
	}, nil
}
