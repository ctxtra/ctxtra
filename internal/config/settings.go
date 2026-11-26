package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Setting changes one program setting. nil removes it, restoring its
// default. Unknown keys and values of the wrong type leave the text intact.
func (e *Editor) Setting(key string, value *yaml.Node) error {
	top, child, _ := strings.Cut(key, ".")
	switch key {
	case "check-releases", "max-ctx-chars", "session-message.max-skills", "session-message.max-instructions":
	default:
		return fmt.Errorf("unknown setting %q; settings: check-releases, max-ctx-chars, session-message.max-skills, session-message.max-instructions", key)
	}
	if value != nil {
		r := yamlx.NewReader(&yamlx.Doc{}, keyPart(top), yamlx.BadValue)
		if key == "check-releases" {
			r.Bool(value, key)
		} else {
			r.Count(value, key)
		}
		if len(r.Errs) > 0 {
			return r.Errs[0]
		}
	}
	if child != "" {
		if value != nil {
			if _, _, err := e.Put(Section(top), child, value, Place{}); err != nil {
				return err
			}
		} else {
			_, i, err := e.find(Section(top), child)
			if err != nil {
				return err
			}
			if i < 0 {
				return nil
			}
			if err := e.Remove(Section(top), child); err != nil {
				return err
			}
		}
		e.changed = append(e.changed, keyPart(top))
		return nil
	}
	if e.doc.Root.Style&yaml.FlowStyle != 0 {
		return errors.New("the config is written as one flow map ({...}); write its top-level keys in block style, one per line; nothing changed")
	}
	lines := e.lines()
	sp, ok := e.entrySpans(lines, e.doc.Root, false, 0, len(lines))
	if !ok {
		return errors.New("the config's top-level keys can't be told apart; start each on a line of its own; nothing changed")
	}
	want := clone(e.doc.Root)
	at := -1
	for i := 0; i+1 < len(want.Content); i += 2 {
		if want.Content[i].Value == top {
			if at >= 0 {
				return fmt.Errorf("the config has %s twice; fix it first", top)
			}
			at = i / 2
		}
	}
	if at < 0 && value == nil {
		return nil
	}
	if at >= 0 && value != nil && yamlx.Same(want.Content[2*at+1], value) {
		return nil
	}
	from, to, comment := len(lines), len(lines), ""
	if at >= 0 {
		from, to = sp[at].start, sp[at].end
		comment = lineComment(lines[from])
	}
	text := ""
	if value == nil {
		want.Content = slices.Delete(want.Content, 2*at, 2*at+2)
		from, to = cut(lines, sp, at)
	} else {
		n := clone(value)
		clearPositions(n)
		var err error
		text, err = renderPair(yamlx.Text(top), n, indentOf(sp), comment)
		if err != nil {
			return err
		}
		if at >= 0 {
			want.Content[2*at+1] = n
		} else {
			i := topIndex(want, top)
			want.Content = slices.Insert(want.Content, 2*i, yamlx.Text(top), n)
			if i < len(sp) {
				from, to = sp[i].head, sp[i].head
			}
		}
	}
	if err := e.keep(Section(top), splice(lines, from, to, text), want); err != nil {
		return err
	}
	e.changed = append(e.changed, keyPart(top))
	return nil
}
