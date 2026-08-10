package mesa

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// run parses and executes source, capturing stdout.
func run(t *testing.T, src string) string {
	t.Helper()
	m, err := ParseSource(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if err := NewInterp(&buf).Run(m); err != nil {
		t.Fatalf("run: %v", err)
	}
	return buf.String()
}

func TestExpressions(t *testing.T) {
	cases := []struct{ src, want string }{
		{`T: PROGRAM = BEGIN WriteInt[2 + 3 * 4] END.`, "14"},
		{`T: PROGRAM = BEGIN WriteInt[(2 + 3) * 4] END.`, "20"},
		{`T: PROGRAM = BEGIN WriteInt[17 MOD 5] END.`, "2"},
		{`T: PROGRAM = BEGIN WriteInt[17 / 5] END.`, "3"},
		{`T: PROGRAM = BEGIN WriteReal[7.0 / 2.0] END.`, "3.5"},
		{`T: PROGRAM = BEGIN WriteBool[3 < 5 AND 2 # 2] END.`, "FALSE"},
		{`T: PROGRAM = BEGIN WriteBool[NOT (1 = 2)] END.`, "TRUE"},
		{`T: PROGRAM = BEGIN WriteInt[IF 3 > 2 THEN 10 ELSE 20] END.`, "10"},
		{`T: PROGRAM = BEGIN WriteString["ab" + "cd"] END.`, "abcd"},
		{`T: PROGRAM = BEGIN WriteInt[ABS[-9]] END.`, "9"},
		{`T: PROGRAM = BEGIN WriteInt[MAX[3, 8, 5]] END.`, "8"},
	}
	for _, c := range cases {
		if got := run(t, c.src); got != c.want {
			t.Errorf("src %q: got %q want %q", c.src, got, c.want)
		}
	}
}

func TestControlFlow(t *testing.T) {
	src := `T: PROGRAM = BEGIN
	  sum: INTEGER ← 0;
	  i: INTEGER;
	  FOR i IN [1..5] DO sum ← sum + i ENDLOOP;
	  WriteInt[sum];
	END.`
	if got := run(t, src); got != "15" {
		t.Errorf("FOR sum: got %q want 15", got)
	}

	// EXIT out of a bare DO loop
	src2 := `T: PROGRAM = BEGIN
	  n: INTEGER ← 0;
	  DO
	    n ← n + 1;
	    IF n = 3 THEN EXIT;
	  ENDLOOP;
	  WriteInt[n];
	END.`
	if got := run(t, src2); got != "3" {
		t.Errorf("EXIT: got %q want 3", got)
	}
}

func TestSelect(t *testing.T) {
	src := `T: PROGRAM = BEGIN
	  Grade: PROCEDURE [n: INTEGER] RETURNS [STRING] = BEGIN
	    SELECT n FROM
	      1, 2, 3 => RETURN ["low"];
	      4, 5    => RETURN ["mid"];
	      ENDCASE => RETURN ["high"];
	  END;
	  WriteString[Grade[2]]; WriteString[","];
	  WriteString[Grade[5]]; WriteString[","];
	  WriteString[Grade[9]];
	END.`
	if got := run(t, src); got != "low,mid,high" {
		t.Errorf("SELECT: got %q", got)
	}
}

// TestSamples executes every bundled sample and checks it does not error
// and produces a known-good line of output.
func TestSamples(t *testing.T) {
	golden := map[string]string{
		"HelloWorld.mesa": "Hello from Mesa!\nRunning on a Go interpreter.\n",
		"Shapes.mesa":     "shape at (1, 2), color green, 3 sides\nshape at (10, 20), color blue, 4 sides\n",
		"Primes.mesa":     "Primes up to 50: 2 3 5 7 11 13 17 19 23 29 31 37 41 43 47\n",
	}
	files, _ := filepath.Glob("samples/*.mesa")
	if len(files) == 0 {
		t.Fatal("no samples found")
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		got := run(t, string(src))
		if want, ok := golden[filepath.Base(f)]; ok && got != want {
			t.Errorf("%s:\n got:  %q\n want: %q", f, got, want)
		}
	}
}
