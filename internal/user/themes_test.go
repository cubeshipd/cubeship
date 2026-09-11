package user

import (
	"os"
	"regexp"
	"slices"
	"testing"
)

// stylesheet is where the dashboard keeps the palettes, from this
// package. They are `:root[data-theme="<name>"]` blocks in it.
const stylesheet = "../../web/src/app/globals.css"

// paletteBlock finds the name each of those blocks is keyed on.
var paletteBlock = regexp.MustCompile(`:root\[data-theme="([a-z0-9-]+)"\]`)

// **Adding a palette is two edits, and this is what makes two safe.**
//
// It is the same shape as the faces, and for the same reason: the list
// lives here because the daemon is what refuses a name, and the colours
// live in the dashboard's own image, which the daemon cannot read. The
// two cannot be derived from each other, so the only thing left is to
// notice when they disagree.
//
// Each way of disagreeing is quiet, which is what makes it worth a
// test rather than a habit:
//
//   - A name here with no block there **saves and then does nothing.**
//     The picker offers it, the click writes it to the account, the
//     attribute lands on <html> and matches no rule — so the interface
//     stays exactly as it was and the one setting whose feedback *is*
//     the result appears to be broken.
//   - A block there with no name here is a palette nobody can choose.
//     `ValidTheme` refuses the name, `PATCH /users/me` answers 400, and
//     the colours sit in the stylesheet shipped to every browser.
//
// `DefaultTheme` is the one exemption, and it is not a hole in the
// rule: `:root` is that palette, and every block here is something
// overriding it. A block of its own would be the default's colours
// written twice.
func TestEveryPaletteIsBothANameAndAStylesheetBlock(t *testing.T) {
	css, err := os.ReadFile(stylesheet)
	if err != nil {
		t.Fatalf("read %s: %v", stylesheet, err)
	}

	var styled []string
	for _, m := range paletteBlock.FindAllStringSubmatch(string(css), -1) {
		if !slices.Contains(styled, m[1]) {
			styled = append(styled, m[1])
		}
	}
	slices.Sort(styled)

	named := slices.Clone(Themes)
	named = slices.DeleteFunc(named, func(t string) bool { return t == DefaultTheme })
	slices.Sort(named)

	if !slices.Equal(styled, named) {
		t.Errorf("globals.css styles %v and user.Themes is %v\n"+
			"a name with no block saves and changes nothing; a block with no name cannot be chosen",
			styled, named)
	}
}

// A palette name goes onto `<html data-theme>` and into a CSS attribute
// selector, and the daemon is the last thing between somebody's input
// and both.
func TestAPaletteIsNamedAndNotAddressed(t *testing.T) {
	for _, name := range Themes {
		if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(name) {
			t.Errorf("%q is not a palette name", name)
		}
	}
	for _, bad := range []string{`" onload="`, "../secret", "Mono", "helix "} {
		if ValidTheme(bad) {
			t.Errorf("ValidTheme(%q) = true", bad)
		}
	}
	// Empty is the default, and it is the one value that is accepted
	// while being no name at all.
	if !ValidTheme("") || !ValidTheme(DefaultTheme) {
		t.Error("the default palette is refused under one of its two names")
	}
}
