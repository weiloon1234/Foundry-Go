// Command queryresources exercises typed field methods with reflective email
// templates linked, so measurements cannot rely on linker dead-code removal.
package main

import (
	"context"
	"fmt"
	"os"

	"foundry.test/consumer/productionprofile"
	"github.com/weiloon1234/Foundry-Go/email"
)

type content struct{ Message string }

func check() error {
	if err := productionprofile.Check(); err != nil {
		return err
	}
	if err := productionprofile.CheckJoinedFields(); err != nil {
		return err
	}
	template, err := email.NewTemplate[content](email.TemplateSource{
		Subject: "resource profile", Text: "{{.Message}}", HTML: "<p>{{.Message}}</p>", MaxBytes: 4096,
	})
	if err != nil {
		return err
	}
	body, err := template.Render(context.Background(), content{Message: "typed <fields>"})
	if err != nil {
		return err
	}
	if body.Text != "typed <fields>" || body.HTML != "<p>typed &lt;fields&gt;</p>" {
		return fmt.Errorf("template rendering changed")
	}
	return nil
}

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
