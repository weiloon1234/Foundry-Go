package generate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeveloperScaffoldsGenerateAndCompileInFreshConsumer(t *testing.T) {
	dir := fixture(t, "package sample\n")
	for _, options := range []ScaffoldOptions{
		{Kind: ModelScaffold, Name: "Widget", Table: "widgets"},
		{Kind: DTOScaffold, Name: "WidgetResponse"},
		{Kind: JobScaffold, Name: "DeliverWidget", ID: "widget.deliver", Queue: "deliveries"},
		{Kind: CommandScaffold, Name: "InspectWidgets", ID: "widgets.inspect"},
	} {
		options.Dir = dir
		path, err := Scaffold(t.Context(), options)
		if err != nil {
			t.Fatal(options.Kind, err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(before), generatedHeader) {
			t.Fatal("scaffold became generator-owned")
		}
		if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
			t.Fatal(options.Kind, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before) {
			t.Fatal("generation changed consumer-owned scaffold", err)
		}
		if _, err := Scaffold(t.Context(), options); err == nil {
			t.Fatal("existing scaffold overwritten")
		}
	}
	write(t, dir, "scaffolds_test.go", `package sample
import (
 "context"
 "errors"
 "io"
 "strings"
 "testing"
 "github.com/weiloon1234/Foundry-Go/cli"
 "github.com/weiloon1234/Foundry-Go/fault"
)
func TestScaffoldBehavior(t *testing.T){
 if _,err:=QueryWidgets().Compile();err!=nil{t.Fatal(err)}
 if err:=WidgetResponseJSON().Validate();err!=nil{t.Fatal(err)}
 if err:=DeliverWidgetJob().Validate();err!=nil{t.Fatal(err)}
 if err:=HandleDeliverWidget(context.Background(),DeliverWidget{});!errors.Is(err,fault.Invalid){t.Fatal("unfinished job succeeded",err)}
 declaration,err:=InspectWidgetsCommand().Declare(NewInspectWidgets);if err!=nil{t.Fatal(err)}
 registry,err:=cli.New(declaration);if err!=nil{t.Fatal(err)}
 invocation,err:=registry.Parse([]string{"widgets.inspect"},io.Discard);if err!=nil{t.Fatal(err)}
 if err:=invocation.Run(t.Context(),nil,cli.Streams{In:strings.NewReader(""),Out:io.Discard,Err:io.Discard});!errors.Is(err,fault.Invalid){t.Fatal("unfinished command succeeded",err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", "./...")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fresh developer scaffolds: %s %v", output, err)
	}
}

func TestDeveloperScaffoldOptionsAndRecoveryAuthority(t *testing.T) {
	for _, options := range []ScaffoldOptions{
		{Kind: ModelScaffold, Name: "Model", Table: "bad; table"},
		{Kind: ModelScaffold, Name: "Model", Table: "records", ID: "ignored"},
		{Kind: DTOScaffold, Name: "DTO", ID: "ignored"},
		{Kind: DTOScaffold, Name: "DTO", Table: "ignored"},
		{Kind: JobScaffold, Name: "Job", ID: "job.id", Queue: "invalid queue"},
		{Kind: CommandScaffold, Name: "../Command", ID: "command.id"},
		{Kind: CommandScaffold, Name: "Command", ID: "command.id", Origin: "ignored"},
	} {
		if err := ValidateScaffold(options); err == nil {
			t.Fatal("invalid developer scaffold options accepted")
		}
	}
	for _, kind := range []ScaffoldKind{ModelScaffold, DTOScaffold, JobScaffold, CommandScaffold} {
		name := "record_" + string(kind) + ".go"
		entry := journalEntry{After: state(oldFile{true, []byte("package sample\n"), 0644})}
		for _, version := range []int{3, 7} {
			log := journal{Version: version, Guards: []string{"."}, Files: map[string]journalEntry{name: entry}}
			data, _ := json.Marshal(log)
			_, err := decodeJournal(data)
			if (err == nil) != (version == 7) {
				t.Fatal("legacy journal authority changed", version, kind, err)
			}
			log.Files[name] = journalEntry{Before: entry.After, After: entry.After}
			data, _ = json.Marshal(log)
			if _, err := decodeJournal(data); err == nil {
				t.Fatal("scaffold journal authorized overwrite")
			}
		}
		if !scaffoldName.MatchString(filepath.Base(name)) {
			t.Fatal("valid new scaffold rejected")
		}
	}
}
