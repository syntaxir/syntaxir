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

// TestGeneratedGoMemoEnvironmentBudget checks the logical compute units of the memo and the
// environment in the Go library: a call counts its unit, and the walks of the memo and of the
// environment, keys compared, stores and the growth of their storage count nothing
// (spec/runtime/parsing.md, Compute Units).
func TestGeneratedGoMemoEnvironmentBudget(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated memo/environment consumers")
	}
	source := `type Word = { Text string }

var v string
rule main Word = t:({v := "a"} &word {v := "b"} word / {v := "b"} word) #atomic => Word{Text: text($t)}

rule word Word = t:(x:[a-z]+ #atomic {text($x) == v}) => Word{Text: text($t)}`
	// Each helper has a callee and two references, making more than 64
	// rules eligible for deferred memoization in the emitted runtime.
	var alternatives []string
	for i := 0; i < 70; i++ {
		name := fmt.Sprintf("spare%d", i)
		source += "\nrule " + name + " Word = word"
		alternatives = append(alternatives, name, name)
	}
	source += "\nrule spare_root Word = " + strings.Join(alternatives, " / ")
	g, err := parseTest(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoMod("memofixture")), 0644); err != nil {
		t.Fatal(err)
	}
	pkg := "memo"
	code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, pkg)
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	fixture := strings.NewReplacer("PACKAGE", pkg).Replace(generatedGoMemoConsumer)
	for name, data := range map[string][]byte{"parser.go": code, "memo_test.go": []byte(fixture)} {
		if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated memo/env accounting: %v\n%s", err, out)
	}
}

const generatedGoMemoConsumer = `package PACKAGE

import sxirrt "syntaxir.dev/sxir/pkg/runtime/libgo"
import("context";"errors";"fmt";"reflect";"strings";"testing")
func catchMemoFatal(f func())(err error){defer func(){if x:=recover();x!=nil{v,ok:=x.(sxirrt.X_fatal);if !ok{panic(x)};err=v.X_err}}();f();return nil}
// unitsParser returns p counting logical compute units under the compute limit limit.
func unitsParser(p *sxirrt.Parser,limit uint64)*sxirrt.Parser{p.X_startUnits(sxirrt.X_parseOptions{X_controls:true,X_unitsLimit:limit});return p}
func makeEnvParser(limit uint64)*sxirrt.Parser{
 p:=unitsParser(&sxirrt.Parser{},limit)
 vals:=make([]any,300);for i:=range vals{vals[i]=i}
 p.X_envDepth=sxirrt.X_envDepthOf(300);p.Env=sxirrt.X_newEnv(vals,p.X_envDepth)
 return p
}
func TestEnvironmentWalksAndAtomicBind(t *testing.T){
 for _,run:=range []struct{name string;f func(*sxirrt.Parser)}{
  {"key",func(p *sxirrt.Parser){p.X_envValues([]int{0})}},
  {"lookup",func(p *sxirrt.Parser){(&sxirrt.ActCtx{P:p}).Var(0)}},
  {"typed lookup",func(p *sxirrt.Parser){(&sxirrt.TActCtx{P:&sxirrt.TParser{Parser:p}}).Var(0)}},
  {"bind",func(p *sxirrt.Parser){p.X_bind(0,777)}},
 }{
  // Gathering a key, reading and binding a variable count nothing, however deep the environment:
  // the call counts the variables of its key (KeyUnits), and a binding of a scalar builds no list or
  // string. Each runs whole under a limit of one unit and publishes a new environment only by a bind.
  p:=makeEnvParser(1);head:=p.Env;before:=append([]any(nil),head.X_vals...)
  if err:=catchMemoFatal(func(){run.f(p)});err!=nil || p.UnitsNow()!=0{t.Errorf("%s counted %d units: %v",run.name,p.UnitsNow(),err)}
  for i,v:=range before{if head.X_vals[i]!=v{t.Fatal("environment changed in place")}}
  saved:=&sxirrt.Parser{Env:head,X_envDepth:p.X_envDepth}
  if run.name=="bind" {if (&sxirrt.ActCtx{P:p}).Var(0)!=777 || saved.X_variable(0)!=0{t.Fatal("binding changed saved environment")}} else {if p.Env!=head || (&sxirrt.ActCtx{P:p}).Var(0)!=0{t.Fatal("lookup changed the environment")}}
 }
}
// A call that finds its entry behind 300 others counts its one unit and nothing for the walk.
func TestNodeMemoLookupWalk(t *testing.T){
 tail:=&sxirrt.X_memoEntry{X_rule:7,X_node:func()*Node{n:=new(Node);sxirrt.D(n).Text="kept";return n}(),X_ok:true}
 head:=tail;for i:=0;i<300;i++{head=&sxirrt.X_memoEntry{X_rule:8,X_next:head}}
 p:=unitsParser(&sxirrt.Parser{},1);p.X_memo.X_slots=[]*sxirrt.X_memoEntry{head}
 r:=&sxirrt.Rule{Id:7,Seen:-1,Memo:true}
 var got *Node
 var ok bool
 if err:=catchMemoFatal(func(){got,ok=p.Call(r,0)});err!=nil||!ok||got!=tail.X_node{t.Fatalf("Node memo walk counted: value=%T error=%v",got,err)}
 if p.UnitsNow()!=1{t.Fatalf("the call counts %d units, want 1",p.UnitsNow())}
 if p.X_memo.X_slots[0]!=head || tail.X_next!=nil{t.Fatal("lookup changed table")}
}
func TestTypedMemoWalksAndAtomicReplacement(t *testing.T){
 tail:=&sxirrt.X_tmemoEntry{X_rule:7,X_val:"kept",X_ok:true}
 head:=tail;for i:=0;i<300;i++{head=&sxirrt.X_tmemoEntry{X_rule:8,X_next:head}}
 for _,put:=range []bool{false,true}{
  // A walk over 300 entries counts nothing, for a lookup and for a store.
  p:=&sxirrt.TParser{Parser:unitsParser(&sxirrt.Parser{},1),X_memoSlots:[]*sxirrt.X_tmemoEntry{head}}
  replacement:=&sxirrt.X_tmemoEntry{X_rule:-1,X_val:"replacement",X_ok:true}
  var got *sxirrt.X_tmemoEntry
  err:=catchMemoFatal(func(){if put{p.X_memoPut(7,0,0,nil,false,replacement)}else{got=p.X_memoGet(7,0,0,nil,false)}})
  if err!=nil||p.UnitsNow()!=0{t.Fatalf("typed memo put=%v counted %d units: %v",put,p.UnitsNow(),err)}
  if put{if p.X_memoGet(7,0,0,nil,false)!=replacement{t.Fatal("typed put lost its entry")}}else{if got!=tail || p.X_memoSlots[0]!=head || tail.X_next!=nil{t.Fatal("typed read changed the table")}}
 }
}
func TestMemoKeysAndStorage(t *testing.T){
 for _,typed:=range []bool{false,true}{
  p:=unitsParser(&sxirrt.Parser{},1);tp:=&sxirrt.TParser{Parser:p}
  get:=func(key []any)any{if typed{return tp.X_memoGet(7,0,0,key,false)};return p.X_memoGet(7,0,0,key,false)}
  put:=func(key []any)any{if typed{e:=&sxirrt.X_tmemoEntry{};tp.X_memoPut(7,0,0,key,false,e);return e};e:=&sxirrt.X_memoEntry{};p.X_memoPut(7,0,0,key,false,e);return e}
  var typedNil *Node
  entry:=put([]any{typedNil})
  if !reflect.ValueOf(get([]any{nil})).IsNil() || get([]any{typedNil})!=entry{t.Fatal("typed nil key equality changed")}
  empty:=put([]any{})
  if get(nil)!=empty{t.Fatal("nil/empty key equality changed")}
  key:=make([]any,400);for i:=range key{key[i]=i}
  // Comparing a long key, and a long string in a key, counts nothing: the call counted its key.
  entry=put(key)
  var got any
  if err:=catchMemoFatal(func(){got=get(key)});err!=nil||got!=entry||p.UnitsNow()!=0{t.Fatal("long key comparison counted",err,p.UnitsNow())}
  long:=strings.Repeat("prefix",600);put([]any{long+"x"})
  if err:=catchMemoFatal(func(){got=get([]any{long+"y"})});err!=nil||!reflect.ValueOf(got).IsNil()||p.UnitsNow()!=0{t.Fatal("string key comparison counted",err,p.UnitsNow())}
 }
 // A store counts nothing, also where it grows the slot array to a far position (growing the
 // storage of a parse is no event), so a limit of one unit does not stop it.
 for _,typed:=range []bool{false,true}{
  store:=func(slots int)(uint64,error){
   p:=unitsParser(&sxirrt.Parser{},1)
   nodeOriginal:=&sxirrt.X_memoEntry{X_rule:3};nodeReplacement:=&sxirrt.X_memoEntry{X_rule:-1}
   p.X_memo.X_slots=make([]*sxirrt.X_memoEntry,slots);p.X_memo.X_slots[0]=nodeOriginal
   typedOriginal:=&sxirrt.X_tmemoEntry{X_rule:3};typedReplacement:=&sxirrt.X_tmemoEntry{X_rule:-1}
   tp:=&sxirrt.TParser{Parser:p,X_memoSlots:make([]*sxirrt.X_tmemoEntry,slots)};tp.X_memoSlots[0]=typedOriginal
   put:=func(){if typed{tp.X_memoPut(7,600,0,nil,false,typedReplacement)}else{p.X_memoPut(7,600,0,nil,false,nodeReplacement)}}
   if err:=catchMemoFatal(put);err!=nil{return 0,err}
   used:=p.UnitsNow()
   if p.X_memo.X_slots[0]!=nodeOriginal || tp.X_memoSlots[0]!=typedOriginal{t.Fatal("slot growth lost an entry")}
   if typed{if len(tp.X_memoSlots)<601{t.Fatal("typed slots did not grow")};tp.X_memoPut(7,600,0,nil,false,typedReplacement);if tp.X_memoGet(7,600,0,nil,false)!=typedReplacement || typedReplacement.X_next!=nil{t.Fatal("typed self replacement changed")}}else{if len(p.X_memo.X_slots)<601{t.Fatal("Node slots did not grow")};p.X_memoPut(7,600,0,nil,false,nodeReplacement);if p.X_memoGet(7,600,0,nil,false)!=nodeReplacement || nodeReplacement.X_next!=nil{t.Fatal("Node self replacement changed")}}
   return used,nil
  }
  grown,err:=store(300);if err!=nil{t.Fatalf("typed=%v: slot growth stopped at a limit of one unit: %v",typed,err)}
  within,err:=store(601);if err!=nil{t.Fatal(err)}
  if grown!=0||within!=0{t.Fatalf("typed=%v: a store that grows the slots counts %d units, one that does not %d, want none",typed,grown,within)}
 }
}
func TestFirstCallStoragePublication(t *testing.T){
 for _,reuse:=range []bool{false,true}{
  // Recording a first call counts nothing (the call counts its unit), also where the dense words
  // grow, whether or not they come from spare capacity.
  p:=unitsParser(&sxirrt.Parser{Pos:64000,X_deferMemo:true},1)
  p.X_memo.X_stride=1;p.X_memo.X_calls=[]sxirrt.X_seenCalls{{X_calls:17,X_repeats:1}}
  if reuse{p.X_memo.X_seen=make([]uint64,0,2048);for i:=range p.X_memo.X_seen[:cap(p.X_memo.X_seen)]{p.X_memo.X_seen[:cap(p.X_memo.X_seen)][i]=^uint64(0)}}else{p.X_memo.X_seen=make([]uint64,512)}
  r:=&sxirrt.Rule{Seen:0}
  var first bool
  if err:=catchMemoFatal(func(){first=p.FirstCall(r)});err!=nil{t.Fatal("seen growth stopped",err)}
  if p.UnitsNow()!=0{t.Fatalf("a first call that grows the seen words counts %d units, want none",p.UnitsNow())}
  if !first || p.X_memo.X_calls[0].X_calls!=18{t.Fatal("first call changed")}
  if p.X_memo.X_seen[0]!=0 || p.X_memo.X_seen[1000]!=1{t.Fatal("reused unseen words were not zeroed before stamping")}
  if p.FirstCall(r){t.Fatal("repeated call deferred again")}
 }
}
func TestMemoCancellation(t *testing.T){
 // A bind and a memo lookup count nothing and make no check, so they run whole after the context
 // is canceled; the next observation of the context sees it.
 ctx,cancel:=context.WithCancel(context.Background())
 p:=makeEnvParser(0);p.X_startUnits(sxirrt.X_parseOptions{X_controls:true,X_unitsCtx:ctx})
 cancel()
 before:=p.Env
 if err:=catchMemoFatal(func(){p.X_bind(0,999)});err!=nil || (&sxirrt.ActCtx{P:p}).Var(0)!=999{t.Fatal("bind observed the context",err)}
 saved:=&sxirrt.Parser{Env:before,X_envDepth:p.X_envDepth}
 if saved.X_variable(0)!=0{t.Fatal("bind changed the saved environment")}
 tail:=&sxirrt.X_memoEntry{X_rule:7};head:=tail;for i:=0;i<600;i++{head=&sxirrt.X_memoEntry{X_rule:8,X_next:head}}
 p.X_memo.X_slots=[]*sxirrt.X_memoEntry{head}
 var got *sxirrt.X_memoEntry
 if err:=catchMemoFatal(func(){got=p.X_memoGet(7,0,0,nil,false)});err!=nil||got!=tail{t.Fatal("memo lookup observed the context",err)}
 if p.X_memo.X_slots[0]!=head{t.Fatal("lookup mutated table")}
 if err:=catchMemoFatal(func(){p.X_unitsObserve()});!errors.Is(err,context.Canceled){t.Fatal("the next observation missed the cancellation",err)}
}

func TestSparseFirstCallInterruptionAndRetry(t *testing.T){
 if sxirGrammar.X_nseen<=64{t.Fatal("sparse interruption fixture has too few seen rules for sparse tracking")}
 for _,at:=range []int{600,1200}{
  // Growing the sparse directory and its pages counts nothing, nor does recording the first call.
  p:=unitsParser(&sxirrt.Parser{Pos:at*1024,X_deferMemo:true},1)
  p.X_memo.X_stride=65;p.X_memo.X_sparse=true;p.X_memo.X_calls=make([]sxirrt.X_seenCalls,65)
  p.X_memo.X_calls[0]=sxirrt.X_seenCalls{X_calls:17,X_repeats:1}
  p.X_memo.X_pages=make([]sxirrt.X_seenRulePages,65)
  pages:=make([]*[16]uint64,300)
  retained:=&[16]uint64{7};pages[0]=retained
  p.X_memo.X_pages[0]=sxirrt.X_seenRulePages{X_pages:pages,X_last:retained,X_at:0}
  r:=&sxirrt.Rule{Seen:0}
  var first bool
  if err:=catchMemoFatal(func(){first=p.FirstCall(r)});err!=nil{t.Fatal("sparse directory growth stopped",err)}
  if p.UnitsNow()!=0{t.Fatalf("a first call that grows the sparse directory counts %d units, want none",p.UnitsNow())}
  if len(p.X_memo.X_seen)!=0{t.Fatal("sparse invocation allocated a dense table")}
  if !first || p.X_memo.X_calls[0].X_calls!=18 || p.FirstCall(r){t.Fatal("sparse first call or repeat detection changed")}
  if p.X_memo.X_pages[0].X_pages[0]!=retained || retained[0]!=7 || len(p.X_memo.X_seen)!=0{t.Fatal("sparse retry corrupted earlier pages or allocated dense storage")}
 }
}

func TestPublicEnvironmentParity(t *testing.T){
 for _,unit:=range []PositionUnit{Bytes,CodePoints}{for _,input:=range []string{"a","b","aa","bb","?",""}{
  base,baseErr:=Parse(input,WithUnit(unit));before:="";if base!=nil{before=base.String()}
  got,err:=Parse(input,WithUnit(unit),WithLimits(Limits{Compute:100000}))
  if fmt.Sprint(err)!=fmt.Sprint(baseErr) || (got!=nil && got.String()!=before){t.Fatal("public memo/env changed",input,err,baseErr)}
  typed,typedErr:=ParseValue(input,WithUnit(unit));controlled,controlledErr:=ParseValue(input,WithUnit(unit),WithLimits(Limits{Compute:100000}))
  if !reflect.DeepEqual(typed,controlled)||fmt.Sprint(typedErr)!=fmt.Sprint(controlledErr){t.Fatal("typed environment changed")}
 }}
}
`
