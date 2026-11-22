package print_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedTypedDocumentPublicationClone checks the CloneValue copy of a typed Document value: it
// keeps identities and aliases, shares nothing with the Document, counts no compute unit and observes
// the context of a conversion and the memory limit through its control.
func TestGeneratedTypedDocumentPublicationClone(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := parseTest(`type Token = { Text string }
type Document = { Text string }
type ParseStats = { Text string }
type Span = { Text string }
type Link = { Label *string; Next *Link }
type Alt ~ Token | Link
type Box = { First Token; Again Token; Items []Token; AgainItems []Token; Optional []*Token; Nested [][]Token; Empty []Token; NilList []Token; Choice Alt; Link *Link }
rule main = x:token => Box{First: $x, Again: $x, Items: []Token{$x, $x}, Optional: []*Token{nil, $x}, Nested: [][]Token{[]Token{$x}}, Choice: $x}
rule token Token = t:("x") #atomic => Token{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	// The types are named like the names of the parser package (Document, ParseStats, Span) and live
	// in their own package, so that they compile next to them.
	dir := t.TempDir()
	codegentest.GeneratePackage(t, dir, "doc", g, common.GenOptions{Start: "main", Types: true, Document: true})
	ordinary := codegentest.GeneratePackage(t, dir, "ordinary", g, common.GenOptions{Start: "main", Types: true})
	if strings.Contains(ordinary, "type typedDocumentClone") {
		t.Fatalf("default generation includes Document cloning")
	}
	for name, text := range map[string]string{
		"go.mod": codegentest.GoMod("codegentest"),
		"doc/parser_test.go": `package doc
import (
	sxirrt "syntaxir.dev/sxir/pkg/runtime/libgo"
 "codegentest/doc/types"
 "context"
 "errors"
 "testing"
)
func TestPublicationOwnershipAndAliases(t *testing.T) {
 token := &types.Token{Text:"private"}
 label := "private"
 link := &types.Link{Label:&label}
 link.Next = link
 items := make([]*types.Token,2,4)
 items[0],items[1]=token,token
 input := &types.Box{First:token,Again:token,Items:items,AgainItems:items,Optional:[]*types.Token{nil,token},Nested:[][]*types.Token{items},Empty:make([]*types.Token,0,8),Choice:token,Link:link}
 c := &typedDocumentClone{}
 output := c.toTypeBox(input)
 c.Finish()
 c.Release()
 if output==input || output.First==token || output.First!=output.Again || output.Items[0]!=output.First || output.Optional[0]!=nil || output.Optional[1]!=output.First || output.Choice.(*types.Token)!=output.First {t.Fatal("typed identity changed")}
 if &output.Items[0]!=&output.AgainItems[0] || &output.Items[0]!=&output.Nested[0][0] {t.Fatal("shared slice views changed")}
 if output.Empty==nil || output.NilList!=nil {t.Fatal("nil and empty changed")}
 if output.Link==link || output.Link.Next!=output.Link || output.Link.Label==&label {t.Fatal("recursive graph or scalar pointer changed")}
 output.First.Text="public"
 *output.Link.Label="public"
 if token.Text!="private" || label!="private" {t.Fatal("publication exposed memo values")}
 if c.Control!=nil || c.Values!=nil || c.Slices!=nil || c.Tasks!=nil {t.Fatal("publication retained private metadata")}
}
func TestPublicationDeepGraph(t *testing.T) {
 var input *types.Link
 for i:=0;i<100001;i++ {input=&types.Link{Next:input}}
 c := &typedDocumentClone{}
 output:=c.toTypeLink(input)
 c.Finish()
 c.Release()
 count:=0
 for output!=nil {count++;output=output.Next}
 if count!=100001 {t.Fatalf("deep clone length: %d",count)}
}
func cloneWithControl(input *types.Box, c *typedDocumentClone,beforeFinish ...func())(output *types.Box,err error) {
 defer sxirrt.X_recoverParse(&output,&err)
 defer c.Release()
 output=c.toTypeBox(input)
 if len(beforeFinish)>0 {beforeFinish[0]()}
 c.Finish()
 return output,nil
}
func TestPublicationControls(t *testing.T) {
 input:=&types.Box{First:&types.Token{Text:"private"},Items:make([]*types.Token,1000)}
 // A compute limit gives the copy no control (the copy is Document work that no limit counts), and
 // a memory limit far above the copy does not interrupt it.
 if sxirrt.X_newBudgetControl(Limits{Compute:1}).X_memoryControl()!=nil {t.Fatal("a compute limit gave the copy a control")}
 c:=&typedDocumentClone{TypedDocumentCloneBase: sxirrt.TypedDocumentCloneBase{Control: sxirrt.X_newBudgetControl(Limits{Compute:1,Memory:1<<40})}}
 if output,err:=cloneWithControl(input,c);output==nil || err!=nil {t.Fatalf("a memory limit above the copy interrupted it: %v %v",output,err)}
 // The control of a conversion carries the context of the parse, which the copy observes.
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 control,done:=sxirrt.X_conversionControl(ctx,nil)
 c=&typedDocumentClone{TypedDocumentCloneBase: sxirrt.TypedDocumentCloneBase{Control: control}}
 if output,err:=cloneWithControl(input,c);output!=nil || !errors.Is(err,context.Canceled) {t.Fatalf("cancel: %v %v",output,err)}
 done()
 if c.Control!=nil || c.Values!=nil || c.Slices!=nil || c.Tasks!=nil {t.Fatal("interrupted clone retained state")}
 ctx,cancel=context.WithCancel(context.Background())
 control,done=sxirrt.X_conversionControl(ctx,nil)
 c=&typedDocumentClone{TypedDocumentCloneBase: sxirrt.TypedDocumentCloneBase{Control: control}}
 if output,err:=cloneWithControl(&types.Box{First:&types.Token{}},c,cancel);output!=nil || !errors.Is(err,context.Canceled) {t.Fatalf("final cancellation poll: %v %v",output,err)}
 done()
 if output,err:=cloneWithControl(input,&typedDocumentClone{});output==nil || err!=nil {t.Fatalf("retry: %v",err)}
}
// The conversion of a typed Document parse's result observes the context of the parse: a cancellation
// during the conversion stops it, publishes nothing and leaves the Document able to parse again.
func TestPublicationObservesTheParseContext(t *testing.T) {
 d,err:=NewDocument("x");if err!=nil {t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background())
 o,err:=d.doc.ResolveParseOptions([]ParseOption{WithContext(ctx)});if err!=nil {t.Fatal(err)}
 a:=&tslabs{}
 v,err:=sxirrt.TypedDocumentParse(d.doc,trules[0],o,a,func(v any) any {cancel();a.control.Check();return v})
 if v!=nil || !errors.Is(err,context.Canceled) {t.Fatalf("conversion missed the cancellation: %v %v",v,err)}
 if a.control!=nil {t.Fatal("the conversion kept its control")}
 if v,err:=d.ParseValue();v==nil || err!=nil {t.Fatalf("retry: %v %v",v,err)}
}
func TestPublicationCollidingNames(t *testing.T) {
 c:=&typedDocumentClone{}
 if c.toTypeDocument(&types.Document{Text:"m"}).Text!="m" || c.toTypeParseStats(&types.ParseStats{Text:"e"}).Text!="e" || c.toTypeSpan(&types.Span{Text:"s"}).Text!="s" {t.Fatal("library name collision")}
 c.Finish();c.Release()
}
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", codegentest.GoFlagsEnv)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed Document publication: %v\n%s", err, output)
	}
}
