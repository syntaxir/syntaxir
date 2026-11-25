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

// TestGeneratedGoGraphBudget checks the logical compute units of the library's term compositions over
// value graphs (fields, list members, folds, captures, Operators Rule frames): a list an action builds
// and the elements a fold visits count, before the work they cover, and member operations, the values
// the parse builds and frames count nothing (spec/runtime/parsing.md, Compute Units).
func TestGeneratedGoGraphBudget(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated graph-control consumers")
	}
	g, err := parseTest(`type Leaf = { Text string }

type Bag = {
    Items  []Leaf
    Nested [][]Leaf
}

rule main Bag = x:leaf* #cut => Bag{Items: $x.Value, Nested: list($x.Value, $x.Value)}

rule leaf Leaf = t:("a") #atomic => Leaf{Text: text($t)}

rule rollback Bag = (x:leaf "z" / x:leaf) rest:(part:leaf "b")* last:leaf #cut => Bag{Items: list($x, $last), Nested: list(map($rest, (r) => r.$part))}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoMod("graphfixture")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		pkg := fmt.Sprintf("graph_%v", convert)
		code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, ConvertTypes: convert, Document: true})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, pkg)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		check := ""
		if !convert {
			check = generatedGoGraphTypedRuleCheck
		}
		consumer := strings.NewReplacer("TYPED_RULE_CHECK", check, "PACKAGE", pkg, "CONVERT", fmt.Sprint(convert)).Replace(generatedGoGraphConsumer)
		for name, data := range map[string][]byte{"parser.go": code, "graph_test.go": []byte(consumer)} {
			if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated graph controls: %v\n%s", err, out)
	}
}

// generatedGoGraphTypedRuleCheck needs the typed rule table, which conversion output omits.
const generatedGoGraphTypedRuleCheck = `for _,name:=range []string{"main","rollback"}{found:=false;for _,r:=range trules{if r.Name==name{found=true;if r.Entry==nil || r.Body!=nil{t.Fatal("fixture did not exercise a direct typed body",name)}}};if !found{t.Fatal("missing typed rule",name)}}
`

const generatedGoGraphConsumer = `package PACKAGE

import sxirrt "syntaxir.dev/sxir/pkg/runtime/libgo"
import "syntaxir.dev/sxir/pkg/runtime/value/impl"
import("context";"errors";"fmt";"reflect";"slices";"strings";"testing")
const conversion = CONVERT
// graphParser returns a parser that counts logical compute units under the compute limit limit.
func graphParser(limit uint64)*sxirrt.Parser{p:=&sxirrt.Parser{};p.X_setGrammar(sxirGrammar);p.X_startUnits(sxirrt.X_parseOptions{X_controls:true,X_unitsLimit:limit});return p}
func graphFatal(f func())(err error){defer func(){if x:=recover();x!=nil{v,ok:=x.(sxirrt.X_fatal);if !ok{panic(x)};err=v.X_err}}();f();return nil}
// The helpers below call the action expression compositions as lowered action expressions do: a field read through ActionMember,
// and a fold that counts the elements it visits (UnitsOfItems), then reads the items and steps through them with ActionItemAt.
func nodeMember(p *sxirrt.Parser,x any,name string)any{v,f:=p.ActionMember(&sxirrt.ActCtx{P:p},sxirrt.MemberField,x,name,name);if f!=nil{panic(f)};return v}
func typedMember(tp *sxirrt.TParser,x any,name string)any{v,f:=tp.ActionMember(&sxirrt.TActCtx{P:tp},sxirrt.MemberField,x,name,name);if f!=nil{panic(f)};return v}
func nodeFold(p *sxirrt.Parser,fn string,right bool,acc,list any,step func(acc,item any)any)any{p.UnitsOfItems(list,1);items,f:=p.ActionItems(fn,list);if f!=nil{panic(f)};for i:=range items{acc=step(acc,p.ActionItemAt(items,i,right))};return acc}
func typedFold(tp *sxirrt.TParser,fn string,right bool,acc,list any,step func(acc,item any)any)any{tp.UnitsOfItems(list,1);items,f:=tp.ActionItems(fn,list);if f!=nil{panic(f)};for i:=range items{acc=step(acc,tp.ActionItemAt(items,i,right))};return acc}
func requireGraphLimit(t *testing.T,p *sxirrt.Parser,f func()){
 t.Helper();err:=graphFatal(f)
 if !errors.Is(err,ErrLimit){t.Fatal("operation escaped the compute limit",err)}
}
func requireNoUnits(t *testing.T,p *sxirrt.Parser,what string,f func()){
 t.Helper()
 if err:=graphFatal(f);err!=nil || p.UnitsNow()!=0{t.Fatal(what,"counted",p.UnitsNow(),err)}
}
func dataNode(d sxirrt.Data)*Node{n:=new(Node);*sxirrt.D(n)=d;return n}
func graphKids()([]*Node,[]any){
 nodes:=make([]*Node,300);values:=make([]any,300)
 for i:=range nodes{nodes[i]=dataNode(sxirrt.Data{Start:int32(i+3),End:int32(i+4),Text:"a"});values[i]=&sxirrt.TMatch{TNode:sxirrt.TNode[string]{Span:Span{Start:i+3,End:i+4},Value:"a"}}}
 return nodes,values
}
func TestGraphFields(t *testing.T){
 // Reading and setting a field are member operations, which count nothing (spec/runtime/parsing.md,
 // Compute Units): each runs whole under a limit of one unit.
 nodes,_:=graphKids();fs:=make(valueimpl.Fields,300);tn:=&sxirrt.TSeq{Typ:"tuple",X_fields:make([]sxirrt.X_tfield,300)}
 for i:=range fs{name:=fmt.Sprintf("field%d",i);fs[i]=valueimpl.NodeField{Name:name,Value:i};tn.X_fields[i]=sxirrt.X_tfield{name,i}}
 for _,typed:=range []bool{false,true}{
  p:=graphParser(1);tp:=&sxirrt.TParser{Parser:p};node:=dataNode(sxirrt.Data{Fields:fs})
  var got any
  requireNoUnits(t,p,"field read",func(){if typed{got=typedMember(tp,tn,"field299")}else{got=nodeMember(p,node,"field299")}})
  if got!=299{t.Fatal("field read changed",got)}
  requireNoUnits(t,p,"field setter",func(){if typed{tp.X_setValueField(tn,"field299",nodes[0])}else{p.X_setValueField(&fs,"field299",nodes[0])}})
  if typed{if len(tn.X_fields)!=300 || tn.X_fields[299].X_val!=nodes[0]{t.Fatal("typed replacement")};tn.X_fields[299].X_val=299}else{if len(fs)!=300 || fs[299].Value!=nodes[0]{t.Fatal("Node replacement")};fs[299].Value=299}
 }
}
func TestGraphListsAndOwnership(t *testing.T){
 nodes,values:=graphKids()
 for _,typed:=range []bool{false,true}{
  // A list that an action builds counts one unit for each element (ActionHullLogical), and the list
  // that map builds of a projection counts the elements it visits as well (NewListLogical): both
  // stop before they build under a limit below their units, and publish nothing.
  for _,logical:=range []string{"hull","map"}{
   p:=graphParser(299);tp:=&sxirrt.TParser{Parser:p};var result any
   requireGraphLimit(t,p,func(){if typed{c:=&sxirrt.TActCtx{P:tp};if logical=="hull"{result=tp.ActionHullLogical(c,false,values)}else{result=c.NewListLogical(values)}}else{c:=&sxirrt.ActCtx{P:p};if logical=="hull"{result=p.ActionHullLogical(c,false,nodes)}else{result=c.NewListLogical(nodes)}}})
   if result!=nil{t.Fatal("partial list returned")}
   want:=uint64(300);if logical=="map"{want=600}
   p=graphParser(want);tp=&sxirrt.TParser{Parser:p}
   if err:=graphFatal(func(){if typed{c:=&sxirrt.TActCtx{P:tp};if logical=="hull"{result=tp.ActionHullLogical(c,false,values)}else{result=c.NewListLogical(values)}}else{c:=&sxirrt.ActCtx{P:p};if logical=="hull"{result=p.ActionHullLogical(c,false,nodes)}else{result=c.NewListLogical(nodes)}}});err!=nil || p.UnitsNow()!=want{t.Fatal("list units",logical,p.UnitsNow(),want,err)}
  }
  // The list primitives under them count nothing themselves, and copy their source.
  for _,spanOnly:=range []bool{false,true}{
   p:=graphParser(1);tp:=&sxirrt.TParser{Parser:p}
   if typed{var n *sxirrt.TSeq;requireNoUnits(t,p,"typed list",func(){c:=&sxirrt.TActCtx{P:tp};if spanOnly{n=c.X_listNode(slices.Clone(values)).(*sxirrt.TSeq)}else{n=c.NewList(values).(*sxirrt.TSeq)}});if n.Start!=3 || n.End!=303 || len(n.Kids)!=300{t.Fatal("typed list spans")};n.Kids[0]=nil;if values[0]==nil{t.Fatal("typed list aliases source")}}else{var n *Node;requireNoUnits(t,p,"Node list",func(){c:=&sxirrt.ActCtx{P:p};if spanOnly{n=c.X_listNode(slices.Clone(nodes))}else{n=c.NewList(nodes)}});if sxirrt.D(n).Start!=3 || sxirrt.D(n).End!=303 || len(sxirrt.D(n).Children)!=300{t.Fatal("Node list spans")};sxirrt.D(n).Children[0]=nil;if nodes[0]==nil{t.Fatal("Node list aliases source")}}
  }
  // Taking the gathered values off counts nothing (values the parse builds rather than an action),
  // so it runs to the end under a limit of one unit.
  p:=graphParser(1);tp:=&sxirrt.TParser{Parser:p}
  if typed{tp.KidStack=append([]any(nil),values...);got:=tp.Kids(0);if len(got)!=300{t.Fatal("typed kids")};got[0]=nil;if values[0]==nil{t.Fatal("typed children alias")}}else{p.KidStack=append([]*Node(nil),nodes...);got:=p.Kids(0);if len(got)!=300{t.Fatal("Node kids")};got[0]=nil;if nodes[0]==nil{t.Fatal("Node children alias")}}
  if p.UnitsNow()!=0{t.Fatal("taking values off counted",p.UnitsNow())}
 }
}
func TestGraphCaptureUndoAndCleanup(t *testing.T){
 for _,typed:=range []bool{false,true}{
  p:=graphParser(1);tp:=&sxirrt.TParser{Parser:p}
  n:=dataNode(sxirrt.Data{Text:"old"});changed:=dataNode(sxirrt.Data{Text:"new"});f:=&sxirrt.CaptureFrame{Vals:make([]*Node,300)};tf:=&sxirrt.TCaptureFrame{Vals:make([]any,300)}
  for i:=0;i<300;i++{f.Vals[i]=changed;tf.Vals[i]=changed;p.Trail=append(p.Trail,sxirrt.X_undo{X_f:f,X_slot:i,X_old:n});tp.Trail=append(tp.Trail,sxirrt.X_tundo{X_f:tf,X_slot:i,X_old:n})}
  // Undoing the capture writes and releasing the storage count nothing (captures and the storage of
  // a parse are no event), so a reset undoes all 300 under a limit of one unit.
  if typed{tp.Reset(sxirrt.Mark{});for _,v:=range tf.Vals{if v!=n{t.Fatal("typed undo retry")}};if len(tp.Trail)!=0{t.Fatal("typed trail retained")};if p.UnitsNow()!=0{t.Fatal("typed undo counted",p.UnitsNow())};tp.Trail=append(tp.Trail,sxirrt.X_tundo{X_f:tf,X_old:n});tp.X_recycle();for _,v:=range tp.Trail[:cap(tp.Trail)]{if v.X_f!=nil || v.X_old!=nil{t.Fatal("typed cleanup blocked")}}}else{p.Reset(sxirrt.Mark{});for _,v:=range f.Vals{if v!=n{t.Fatal("Node undo retry")}};if len(p.Trail)!=0{t.Fatal("Node trail retained")};if p.UnitsNow()!=0{t.Fatal("Node undo counted",p.UnitsNow())};p.Trail=append(p.Trail,sxirrt.X_undo{X_f:f,X_old:n});p.X_release();for _,v:=range p.Trail[:cap(p.Trail)]{if v.X_f!=nil || v.X_old!=nil{t.Fatal("Node cleanup blocked")}}}
 }
}
func TestTypedFieldMaterialization(t *testing.T){
 // Reading a list field of a typed value is a member operation: it counts nothing, however many
 // elements the field holds, as reading the same field of a node does.
 bag:=&Bag{Items:make([]*Leaf,300),Nested:make([][]*Leaf,300)}
 for i:=range bag.Items{bag.Items[i]=&Leaf{Span:Span{Start:i+3,End:i+4},Text:"a"};bag.Nested[i]=[]*Leaf{bag.Items[i]}}
 for _,name:=range []string{"Items","Nested"}{
  p:=graphParser(1);tp:=&sxirrt.TParser{Parser:p};var result any
  requireNoUnits(t,p,"list field read",func(){result=typedMember(tp,bag,name)})
  got:=result.(*sxirrt.TSeq);ordinary,_:=bag.TField(name)
  want:=ordinary.(*sxirrt.TSeq)
  if !reflect.DeepEqual(got,want) || got.Start!=3 || got.End!=303{t.Fatal("field materialization changed",name)}
  if name=="Items"{got.Kids[0]=nil;if bag.Items[0]==nil{t.Fatal("field result aliases input")}}
 }
 p:=graphParser(1);tp:=&sxirrt.TParser{Parser:p};empty:=&Bag{Items:[]*Leaf{},Nested:nil}
 if got:=typedMember(tp,empty,"Items");got==nil || len(got.(*sxirrt.TSeq).Kids)!=0{t.Fatal("empty field changed")}
 if got:=typedMember(tp,empty,"Nested");got==nil || len(got.(*sxirrt.TSeq).Kids)!=0{t.Fatal("nil field is not the empty list")}
}
func TestGraphFoldOrderAndBudget(t *testing.T){
 // A fold counts one unit for each element it visits, before it visits any: under a limit below
 // the elements it stops without calling its step.
 nodes,values:=graphKids()
 for _,typed:=range []bool{false,true}{for _,right:=range []bool{false,true}{
  p:=graphParser(299);tp:=&sxirrt.TParser{Parser:p};visits:=0
  f:=func(acc,item any)any{visits++;return item}
  requireGraphLimit(t,p,func(){if typed{typedFold(tp,"foldl",right,nil,&sxirrt.TSeq{Typ:"list",Kids:values},f)}else{nodeFold(p,"foldl",right,nil,dataNode(sxirrt.Data{Kind:sxirrt.ListKind,Children:nodes}),f)}})
  if visits!=0{t.Fatal("fold visited past the limit",visits)}
  p=graphParser(300);tp=&sxirrt.TParser{Parser:p}
  var got any;if typed{got=typedFold(tp,"foldl",right,nil,&sxirrt.TSeq{Typ:"list",Kids:values},f)}else{got=nodeFold(p,"foldl",right,nil,dataNode(sxirrt.Data{Kind:sxirrt.ListKind,Children:nodes}),f)}
  index:=299;if right{index=0};want:=any(nodes[index]);if typed{want=values[index]};if visits!=300 || got!=want || p.UnitsNow()!=300{t.Fatal("fold order or units changed",visits,p.UnitsNow())}
 }}
}
func TestGraphPublicLimitsAndRetainedResults(t *testing.T){
 input:=strings.Repeat("a",150)
 baseline,err:=ParseValue(input);if err!=nil || baseline==nil || len(baseline.Items)!=150 || len(baseline.Nested)!=2{t.Fatal("typed baseline",err)}
 before:=fmt.Sprintf("%#v",baseline.Items[0]);tree,err:=Parse(input);if err!=nil{t.Fatal(err)};treeBefore:=tree.String()
 for _,unit:=range []PositionUnit{Bytes,CodePoints}{
  for _,ast:=range []bool{false,true}{
   call:=func(n uint64)(any,error){opts:=[]ParseOption{WithUnit(unit),WithLimits(Limits{Compute:n})};if ast{return ParseValue(input,opts...)};return Parse(input,opts...)}
   lo,hi:=uint64(1),uint64(1000000)
   for lo<hi{mid:=lo+(hi-lo)/2;v,e:=call(mid);if errors.Is(e,ErrLimit){if !reflect.ValueOf(v).IsZero(){t.Fatal("partial public result")};lo=mid+1}else if e==nil{hi=mid}else{t.Fatal("unexpected public error",e)}}
   if _,e:=call(lo);e!=nil{t.Fatal("exact limit",e)}
   if v,e:=call(lo-1);!errors.Is(e,ErrLimit) || !reflect.ValueOf(v).IsZero(){t.Fatal("one-short result",v,e)}
   ctx,cancel:=context.WithCancel(context.Background());cancel()
   if n,e:=ParseValue(input,WithContext(ctx));n!=nil || !errors.Is(e,context.Canceled){t.Fatal("cancelled typed result",n,e)}
   if n,e:=ParseValue(input);n==nil || e!=nil{t.Fatal("typed retry",e)}
  }
 }
 if tree.String()!=treeBefore || fmt.Sprintf("%#v",baseline.Items[0])!=before{t.Fatal("retained result changed")}
}
func TestGraphCaptureAttachment(t *testing.T){
 names:=make([]string,300);nodes,values:=graphKids()
 for i:=range names{names[i]=fmt.Sprintf("capture%d",i)}
 names[299]=names[0]
 for _,typed:=range []bool{false,true}{
  // Attaching captures counts nothing (values the parse builds rather than an action), so it runs to
  // the end under a limit of one unit.
  p:=graphParser(1);tp:=&sxirrt.TParser{Parser:p}
  if typed{n:=tp.AttachCaptures(nil,names,values,3,303).(*sxirrt.TSeq);if len(n.X_fields)!=299 || n.X_fields[0].X_val!=values[299] || n.X_fields[1].X_name!=names[1]{t.Fatal("typed capture order/replacement")}}else{n:=p.AttachCaptures(nil,names,&sxirrt.CaptureFrame{Vals:nodes},3,303);if len(sxirrt.D(n).Fields)!=299 || sxirrt.D(n).Fields[0].Value!=nodes[299] || sxirrt.D(n).Fields[1].Name!=names[1]{t.Fatal("Node capture order/replacement")}}
  if p.UnitsNow()!=0{t.Fatal("attaching captures counted",p.UnitsNow())}
 }
}
func TestGraphOperatorsRuleSnapshots(t *testing.T){
 names:=make([]string,300);nodes,values:=graphKids()
 for i:=range names{names[i]=fmt.Sprintf("p%d",i)}
 for _,typed:=range []bool{false,true}{
  // Trying an operator and saving and restoring the 300 slots of the frame around the try count
  // nothing (the operators of an Operators Rule and its frames are no event), so the selection runs
  // whole under a limit of one unit.
  p:=graphParser(1);tp:=&sxirrt.TParser{Parser:p};var bestNode sxirrt.X_operatorsRuleAttempt;var bestTyped sxirrt.X_toperatorsRuleAttempt
  err:=graphFatal(func(){if typed{
   op:=&sxirrt.TOperatorsRuleOp{Kind:"prefix",Line:&sxirrt.TOperatorsRuleLine{Scope:names,M:func(p *sxirrt.TParser)(any,bool){for i,v:=range values{p.Frame.Vals[i]=v};p.Pos=1;return values[0],true}}}
   bestTyped=tp.X_longest([]*sxirrt.TOperatorsRuleOp{op})
  }else{
   op:=&sxirrt.OperatorsRuleOp{Kind:"prefix",Line:&sxirrt.OperatorsRuleLine{Scope:names,M:func(p *sxirrt.Parser)(*Node,bool){for i,v:=range nodes{p.Frame.Vals[i]=v};p.Pos=1;return nodes[0],true}}}
   bestNode=p.X_longest([]*sxirrt.OperatorsRuleOp{op})
  }})
  if err!=nil || p.Pos!=0 || p.Frame!=nil || tp.Frame!=nil{t.Fatal("OperatorsRule selection changed state",err)}
  if typed{if bestTyped.X_op==nil || bestTyped.X_end!=1 || !reflect.DeepEqual(bestTyped.X_frame.Vals,values){t.Fatal("typed OperatorsRule snapshot changed")}}else{if bestNode.X_op==nil || bestNode.X_end!=1 || !reflect.DeepEqual(bestNode.X_frame.Vals,nodes){t.Fatal("Node OperatorsRule snapshot changed")}}
  if p.UnitsNow()!=0{t.Fatal("OperatorsRule selection counted",p.UnitsNow())}
 }
}

func TestDirectCaptureRollback(t *testing.T){
 input:="a"+strings.Repeat("ab",150)+"a"
TYPED_RULE_CHECK call:=func(limit uint64)(*Bag,error){var opts []ParseOption;opts=append(opts,WithLimits(Limits{Compute:limit}));v,e:=ParseRuleValue("rollback",input,opts...);if e!=nil{return nil,e};n,ok:=v.(*Bag);if !ok{t.Fatalf("rollback output %T",v)};return n,nil}
 got,err:=call(1000000);if err!=nil || got==nil || len(got.Items)!=2 || len(got.Nested)!=1 || len(got.Nested[0])!=150 || got.Items[0].Start!=0 || got.Items[1].Start!=301{t.Fatal("partial repeat rollback changed output",got,err)}
 lo,hi:=uint64(1),uint64(1000000)
 for lo<hi{mid:=lo+(hi-lo)/2;n,e:=call(mid);if errors.Is(e,ErrLimit){if n!=nil{t.Fatal("partial rollback output")};lo=mid+1}else if e==nil{hi=mid}else{t.Fatal(e)}}
 if n,e:=call(lo-1);n!=nil || !errors.Is(e,ErrLimit){t.Fatal("rollback one-short",n,e)}
 retry,e:=call(lo);if e!=nil || !reflect.DeepEqual(retry,got){t.Fatal("rollback retry changed output",e)}
}
`
