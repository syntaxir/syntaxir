package print_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedGoActionErrorBudget checks that the error of an action keeps its message under limit
// controls and that long names in it count nothing: the message of an error is not an event of the
// compute units, and a variable is a slot (spec/runtime/parsing.md, Compute Units). Under a small
// compute limit each parse ends as it does without one.
func TestGeneratedGoActionErrorBudget(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated action-error consumers")
	}
	rule, variable := strings.Repeat("r", 8192), strings.Repeat("u", 8192)
	cases := []struct {
		name, start, grammar, want string
		recognize                  bool
	}{
		{"prefix", rule, `type A = { N int }
 rule ` + rule + ` A = a:"a" #cut => A{N:1 / (len($a)-1)}`, "action in " + rule + ": division by zero", false},
		{"predicate", "main", `type Word = { Text string }
 var ` + variable + ` bool
 rule main Word = (({` + variable + `} x:leaf) / x:leaf) #cut => $x
 rule leaf Word = t:("a") #atomic => Word{Text: text($t)}`, "", true},
		{"captured", "main", `type A = { N int }
 rule main A = x:` + rule + ` {$x != nil} $$ #cut => $x
 rule ` + rule + ` A = a:"a" #cut => A{N:1 / (len($a)-1)}`, "action in " + rule + ": division by zero", true},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoMod("actionerrorfixture")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		g, err := parseTest(c.grammar)
		if err != nil {
			t.Fatal(err)
		}
		for _, convert := range []bool{false, true} {
			pkg := fmt.Sprintf("action_%s_%v", c.name, convert)
			code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: pkg, Start: c.start, Types: true, Recognize: true, ConvertTypes: convert})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, pkg)
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			consumer := strings.NewReplacer("PACKAGE", pkg, "WANT_ERROR", fmt.Sprintf("%q", c.want), "CHECK_RECOGNIZE", fmt.Sprint(c.recognize)).Replace(generatedGoActionErrorConsumer)
			for name, data := range map[string][]byte{"parser.go": code, "action_test.go": []byte(consumer)} {
				if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated action-error accounting: %v\n%s", err, out)
	}
}

const generatedGoActionErrorConsumer = `package PACKAGE

import sxirrt "syntaxir.dev/sxir/pkg/runtime/libgo"
import("context";"errors";"reflect";"strings";"testing")
const wantError=WANT_ERROR
const recognize=CHECK_RECOGNIZE
func actionCall(mode int,opts...ParseOption)(any,error){switch mode{case 0:n,e:=Parse("a",opts...);return n,e;case 1:n,e:=ParseValue("a",opts...);return n,e;default:return nil,Recognize("a",opts...)}}
func actionZero(v any)bool{return v==nil || reflect.ValueOf(v).IsZero()}
func TestPublicActionErrorAccounting(t *testing.T){
 modes:=[]int{0,1};if recognize{modes=append(modes,2)}
 for _,mode:=range modes{
  n,e:=actionCall(mode);if wantError==""{if e!=nil || mode!=2 && actionZero(n){t.Fatal("predicate fallback baseline",n,e)}}else if e==nil || e.Error()!=wantError || !actionZero(n) || errors.Unwrap(e)!=nil{t.Fatal("baseline error contract",n,e)}
  _,large:=actionCall(mode,WithLimits(Limits{Compute:1<<50}));if (large==nil)!=(e==nil) || large!=nil && large.Error()!=e.Error(){t.Fatal("generous controls changed error",large,e)}
  partial,limited:=actionCall(mode,WithLimits(Limits{Compute:256}))
  if (limited==nil)!=(e==nil) || limited!=nil && limited.Error()!=e.Error() || mode!=2 && (e==nil)==actionZero(partial){t.Errorf("a long name counted units mode=%d result=%T error=%v",mode,partial,limited)}
  ctx,cancel:=context.WithCancel(context.Background());cancel();partial,cancelled:=actionCall(mode,WithContext(ctx));if !errors.Is(cancelled,context.Canceled) || !actionZero(partial){t.Fatal("cancelled result",partial,cancelled)}
  retry,retryErr:=actionCall(mode);if (retryErr==nil)!=(e==nil) || retryErr!=nil && retryErr.Error()!=e.Error() || !reflect.DeepEqual(retry,n){t.Fatal("action error retry changed output",retryErr)}
 }
}
func catchActionText(f func())(text string,err error){defer func(){if x:=recover();x!=nil{switch x:=x.(type){case sxirrt.X_evalError:text=x.X_msg;case sxirrt.X_fatal:err=x.X_err;default:panic(x)}}}();f();return}
func TestActionTextFragmentsAndFlatPrefix(t *testing.T){
 // The error of a failed action is a value that the parse builds: its text counts nothing, under any
 // limit (spec/runtime/parsing.md, Compute Units).
 message:="division by zero"
 for _,parts:=range [][]string{{""},{"","",""},{"a","","λ\xff"},{strings.Repeat("n",8192)},{strings.Repeat("rule",512)}}{
  where:=strings.Join(parts,"");want:="action in "+where+": "+message
  p:=&sxirrt.Parser{};p.X_startUnits(sxirrt.X_parseOptions{X_controls:true,X_unitsLimit:1})
  e:=p.X_wrapEvalError(where,message)
  if e.Error()!=want || errors.Unwrap(e)!=nil || p.UnitsNow()!=0{t.Fatal("flat wrapper contract",e,p.UnitsNow())}
 }
}
func TestActionCompositionErrorText(t *testing.T){
 long:=strings.Repeat("Kind",2048);n:=new(Node);sxirrt.D(n).Kind=sxirrt.KindOf(long,"");tn:=&sxirrt.TSeq{Typ:"tuple",Kids:make([]any,2048)}
 raise:=func(f any){if f!=nil{panic(f)}}
 for _,typed:=range []bool{false,true}{
  p:=&sxirrt.Parser{};tp:=&sxirrt.TParser{Parser:p}
  cases:=[]func(){
   func(){if typed{_,f:=tp.ActionUnary(sxirrt.OpNeg,tn);raise(f)}else{_,f:=p.ActionUnary(sxirrt.OpNeg,n);raise(f)}},
   func(){if typed{_,f:=tp.ActionBinary(sxirrt.OpAdd,tn,1);raise(f)}else{_,f:=p.ActionBinary(sxirrt.OpAdd,n,1);raise(f)}},
   func(){if typed{_,f:=tp.ActionLogic(sxirrt.OpAnd,tn);raise(f)}else{_,f:=p.ActionLogic(sxirrt.OpAnd,n);raise(f)}},
   func(){if typed{_,f:=tp.ActionLogic(sxirrt.OpOr,tn);raise(f)}else{_,f:=p.ActionLogic(sxirrt.OpOr,n);raise(f)}},
   func(){if typed{_,f:=tp.ActionItems(long,1);raise(f)}else{_,f:=p.ActionItems(long,1);raise(f)}},
   func(){fn:=func(...any)any{return nil};if typed{raise(tp.ActionPush(&sxirrt.TActCtx{P:tp},long,fn))}else{raise(p.ActionPush(&sxirrt.ActCtx{P:p},long,fn))}},
  }
  for _,run:=range cases{
   p.X_startUnits(sxirrt.X_parseOptions{});ordinary,e:=catchActionText(run);if e!=nil || ordinary==""{t.Fatal("ordinary helper error",e)}
   p.X_startUnits(sxirrt.X_parseOptions{X_controls:true,X_unitsLimit:1<<50});active,e:=catchActionText(run);if e!=nil || active!=ordinary{t.Fatal("active helper error changed",e)}
   // The text of the error counts nothing: under the least limit the helper fails as it does without one.
   p.X_startUnits(sxirrt.X_parseOptions{X_controls:true,X_unitsLimit:1});limited,e:=catchActionText(run);if e!=nil || limited!=ordinary || p.UnitsNow()!=0{t.Fatal("limited helper error changed",e,p.UnitsNow())}
  }
 }
}
`
