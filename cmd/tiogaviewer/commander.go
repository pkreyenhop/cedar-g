package main

import (
	"os"
	"path/filepath"
	"strings"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"cedarg/internal/tioga"
)

// The Commander is Cedar's CommandTool: a command interpreter (distinct from the
// shell terminal) where you type Cedar-style commands that drive the viewer —
// "Artwork on", "Open Foo.tioga", "Run Foo.mesa", "EditTool Foo.tioga". Output is
// logged above a command input line.

// newCommanderViewer creates a Commander viewer.
func (s *gioUI) newCommanderViewer() *viewer {
	v := &viewer{kind: vkCommander, title: "Commander"}
	v.cmdEd.SingleLine = true
	v.cmdEd.Submit = true
	v.cmdLog = []string{
		"Cedar Commander — type Help for commands.",
	}
	return v
}

// openCommander opens (or focuses) the command tool.
func (s *gioUI) openCommander() { s.addViewer(s.newCommanderViewer()) }

// runCommand interprets one command line and appends its echo and output to the
// log.
func (s *gioUI) runCommand(v *viewer, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	v.cmdLog = append(v.cmdLog, "% "+line)
	fields := strings.Fields(line)
	cmd, args := fields[0], fields[1:]
	log := func(msg string) { v.cmdLog = append(v.cmdLog, msg) }

	switch strings.ToLower(cmd) {
	case "help", "?":
		log("commands:")
		log("  Artwork on|off        render or hide embedded figures")
		log("  Open <file>           open a document, source or folder")
		log("  Run <file.mesa>       run a Mesa/Cedar program")
		log("  EditTool <file>       open a document in the structure editor")
		log("  Clear                 clear this log")
		log("  Quit                  exit")
	case "artwork":
		on := len(args) == 0 || !strings.EqualFold(args[0], "off")
		s.artworkOff = !on
		if on {
			log("artwork on")
		} else {
			log("artwork off")
		}
	case "open":
		if len(args) == 0 {
			log("open: needs a file")
			break
		}
		p := s.resolvePath(args[0])
		s.openFile(p)
		log("opened " + s.relPath(p))
	case "run":
		if len(args) == 0 {
			log("run: needs a .mesa file")
			break
		}
		p := s.resolvePath(args[0])
		out := s.runMesaFile(p)
		for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			log(ln)
		}
	case "edittool":
		if len(args) == 0 {
			log("edittool: needs a document")
			break
		}
		p := s.resolvePath(args[0])
		s.openFile(p)
		if fv := s.findViewer(p); fv != nil && fv.isDoc() {
			s.enterStruct(fv)
			log("editing " + s.relPath(p))
		} else {
			log("edittool: not a document")
		}
	case "clear":
		v.cmdLog = v.cmdLog[:0]
	case "quit":
		log("(use the Quit button to exit)")
	default:
		log("unknown command: " + cmd + " (try Help)")
	}
}

// resolvePath resolves a command argument against the current root.
func (s *gioUI) resolvePath(arg string) string {
	if filepath.IsAbs(arg) {
		return arg
	}
	if s.root != "" {
		return filepath.Join(s.root, arg)
	}
	return arg
}

// runMesaFile reads and runs a Mesa source file, returning its output. A .mesa
// file on disk is a Tioga container whose source is encoded with Xerox control
// codes and special characters (←, ↑, ≠, …); it must be decoded through the
// Tioga code reader before the lexer sees it, exactly as the .mesa content
// viewer does. tioga.Read falls back to treating plain text as code, so this is
// safe whether the file is a Tioga container or already plain source.
func (s *gioUI) runMesaFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "cannot read " + path + ": " + err.Error()
	}
	return runMesa(tioga.Read(data, true).Code)
}

// commanderBody renders the log above a command input line.
func (s *gioUI) commanderBody(gtx C, v *viewer) D {
	gtx.Constraints.Min = gtx.Constraints.Max
	v.cmdEd.SingleLine = true
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return layout.Inset{Top: 4, Right: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				return s.scrollList(gtx, &v.sc, len(v.cmdLog), func(gtx C, i int) D {
					return s.label(gtx, monoFont, font.Normal, font.Regular, termTextSize-2, v.cmdLog[i], cedarBlack, 1)
				})
			})
		}),
		layout.Rigid(hrule),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
				layout.Stacked(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.UniformInset(5).Layout(gtx, func(gtx C) D {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D {
									return s.label(gtx, monoFont, font.Bold, font.Regular, termTextSize-2, "%", cedarBlack, 1)
								})
							}),
							layout.Flexed(1, func(gtx C) D {
								ed := material.Editor(s.th, &v.cmdEd, "type a command…")
								ed.Color = cedarBlack
								ed.HintColor = cedarGreyMid
								ed.Font = monoFont
								ed.TextSize = s.sp(termTextSize - 2)
								return ed.Layout(gtx)
							}),
						)
					})
				}),
			)
		}),
	)
}

// processCommander runs a command when Enter is pressed in the input line.
func (s *gioUI) processCommander(gtx C, v *viewer) {
	for {
		ev, ok := v.cmdEd.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			s.runCommand(v, v.cmdEd.Text())
			v.cmdEd.SetText("")
			gtx.Execute(key.FocusCmd{Tag: &v.cmdEd})
		}
	}
}
