// util.go: misc functions to convert/send http requests/sort maps
package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/anz-bank/sysl-catalog/pkg/nailgun"

	"github.com/anz-bank/sysl/pkg/diagrams"

	"github.com/anz-bank/protoc-gen-sysl/newsysl"

	"github.com/anz-bank/sysl/pkg/syslutil"

	"github.com/joshcarp/mermaid-go/mermaid"

	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/anz-bank/sysl/pkg/cmdutils"
	"github.com/anz-bank/sysl/pkg/sequencediagram"
	"github.com/hashicorp/go-retryablehttp"

	"github.com/anz-bank/sysl/pkg/sysl"
)

// SanitiseOutputName removes characters so that the string can be used as a hyperlink.
func SanitiseOutputName(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "/", "")
}

// rootDirectory converts a path (eg whatever/anotherdir/this.that) to the ../ pattern to get
// back to the original folder that the sysl-catalog command was executed from
func rootDirectory(s string) string {
	var rootPath string
	dir, _ := path.Split(s)
	numberOfLevels := len(strings.Split(dir, "/"))
	for i := 0; i < numberOfLevels; i++ {
		rootPath += "../"
	}
	return rootPath
}
func SortedKeys(m interface{}) []string {
	keys := reflect.ValueOf(m).MapKeys()
	ret := make([]string, 0, len(keys))
	for _, v := range keys {
		ret = append(ret, v.String())
	}
	sort.Strings(ret)
	return ret
}

// NewTypeRef returns a type reference, needed to correctly generate data model diagrams
func NewTypeRef(appName, typeName string) *sysl.Type {
	return &sysl.Type{
		Type: &sysl.Type_TypeRef{
			TypeRef: &sysl.ScopedRef{
				Ref: &sysl.Scope{Appname: &sysl.AppName{
					Part: []string{appName},
				},
					Path: []string{appName, typeName},
				},
			},
		},
	}
}

// TernaryOperator returns the first element if bool is true and the second element is false
func TernaryOperator(condition bool, i ...interface{}) interface{} {
	if condition {
		return i[0]
	}
	return i[1]
}

// createProjectApp returns a "project" app used to make integration diagrams for any "sub module" apps
func createProjectApp(Apps map[string]*sysl.Application) *sysl.Application {
	app := newsysl.Application("")
	app.Endpoints = make(map[string]*sysl.Endpoint)
	app.Endpoints["_"] = newsysl.Endpoint("_")
	app.Endpoints["_"].Stmt = []*sysl.Statement{}
	for key, _ := range Apps {
		app.Endpoints["_"].Stmt = append(app.Endpoints["_"].Stmt, newsysl.StringStatement(key))
	}
	if app.Attrs == nil {
		app.Attrs = make(map[string]*sysl.Attribute)
	}
	if _, ok := app.Attrs["appfmt"]; !ok {
		app.Attrs["appfmt"] = &sysl.Attribute{
			Attribute: &sysl.Attribute_S{S: "%(appname)"},
		}
	}
	return app
}

// createProjectApp returns a "project" app used to make integration diagrams for any "sub module" apps
func createModuleFromSlices(m *sysl.Module, stmnts []string) *sysl.Module {
	ret := &sysl.Module{Apps: make(map[string]*sysl.Application)}
	for _, appName := range stmnts {
		for key, e := range m.GetApps() {
			if Attribute(e, "package") == appName {
				ret.Apps[key] = e
			}
		}
	}

	return ret
}

type Attr interface {
	GetAttrs() map[string]*sysl.Attribute
}

func Attribute(a Attr, query string) string {
	if description := a.GetAttrs()[query]; description != nil {
		return description.GetS()
	}
	return ""
}

func Fields(t *sysl.Type) map[string]*sysl.Type {
	if tuple := t.GetTuple(); tuple != nil {
		return tuple.GetAttrDefs()
	}
	return nil
}

func FieldType(t *sysl.Type) string {
	typeName, typeDetail := syslutil.GetTypeDetail(t)
	if typeName == "primitive" {
		return strings.ToLower(typeDetail)
	}
	if typeName == "sequence" {
		return "sequence of " + typeDetail
	}
	if typeName == "type_ref" {
		return strings.Join(t.GetTypeRef().GetRef().GetPath(), ".")
	}
	if typeName != "" {
		return typeName
	}
	return typeDetail
}

type Namer interface {
	Attr
	GetName() *sysl.AppName
}

// GetAppPackageName returns the package and app name of any sysl application
func GetAppPackageName(a Namer) (string, string) {
	appName := strings.Join(a.GetName().GetPart(), "")
	packageName := appName
	if attr := a.GetAttrs()["package"]; attr != nil {
		packageName = attr.GetS()
	}
	return packageName, appName
}

func ModulePackageName(m *sysl.Module) string {
	for _, key := range SortedKeys(m.GetApps()) {
		app := m.Apps[key]
		packageName, _ := GetAppPackageName(app)
		return packageName
	}
	return ""
}

// Map applies a function to every element in a string slice
func Map(vs []string, funcs ...func(string) string) []string {
	vsm := make([]string, len(vs))
	for i, v := range vs {
		for j, f := range funcs {
			var middle string
			if j == 0 {
				middle = f(v)
				vsm[i] = middle
			}
			vsm[i] = f(middle)
		}

	}
	return vsm
}

// Map applies a function to every element in a string slice
func Filter(vs []string, f func(string) bool) []string {
	vsm := make([]string, 0, len(vs))
	for _, v := range vs {
		if f(v) {
			vsm = append(vsm, v)
		}
	}
	return vsm
}

func AsSet(in []string) map[string]struct{} {
	ret := make(map[string]struct{})
	for _, e := range in {
		ret[e] = struct{}{}
	}
	return ret
}

// RetryHTTPRequest retries the given request
func RetryHTTPRequest(url string) ([]byte, error) {
	client := retryablehttp.NewClient()
	client.Logger = nil
	client.RetryMax = 100
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	return ioutil.ReadAll(resp.Body)
}

// PlantUMLURL returns a PlantUML url
func PlantUMLURL(plantumlService, contents string) (string, error) {
	encoded, err := diagrams.DeflateAndEncode([]byte(contents))
	if err != nil {
		return "", err
	}
	return fmt.Sprint(plantumlService, "/", "svg", "/~1", encoded), nil
}

func HttpToFile(fs afero.Fs, fileName, url string) error {
	if err := fs.MkdirAll(path.Dir(string(fileName)), os.ModePerm); err != nil {
		return err
	}
	out, err := RetryHTTPRequest(string(url))
	if err != nil {
		return err
	}
	if err := afero.WriteFile(fs, string(fileName), out, os.ModePerm); err != nil {
		return err
	}
	return nil
}

func (p *Generator) PlantumlJava(fs afero.Fs, fileName, contents string) error {
	if err := fs.MkdirAll(path.Dir(fileName), os.ModePerm); err != nil {
		return err
	}
	out, err := PlantumlNailGun(contents)
	if err != nil {

	}
	if err := afero.WriteFile(fs, fileName, out, os.ModePerm); err != nil {
		return err
	}
	return nil
}

func PlantumlNailGun(contents string) ([]byte, error) {
	//c1 := exec.Command("echo", fmt.Sprintf(`"""%s"""`, contents))
	//c2 := exec.Command("ng", "net.sourceforge.plantuml.Run", "-tsvg", "-p")
	//r, w := io.Pipe()
	//c1.Stdout = w
	//c2.Stdin = r
	//
	//var b2 bytes.Buffer
	//c2.Stdout = &b2
	//c1.Start()
	//c2.Start()
	//c1.Wait()
	//w.Close()
	//c2.Wait()
	strings.NewReader(contents)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	nailgun.Run(
		[]string{"ng", "net.sourceforge.plantuml.Run", "-tsvg", "-p"},
		[]string{},
		strings.NewReader(contents),
		&stdout,
		&stderr,
	)
	if stderr.Len() == 0 {
		return stdout.Bytes(), nil
	}
	return nil, errors.New(stderr.String())
}

//func PlantUMLCLI(service, dir, wildcard string) (err error, cleanup func()) {
//	dir = strings.TrimRight(dir, "/")
//	indir := dir + wildcard
//	javaCommand := fmt.Sprintf(`java -Djava.awt.headless=true -jar %s -tsvg '%s'`, service, indir)
//	fmt.Println(javaCommand)
//	fmt.Println("bash", "-c", javaCommand)
//	plantuml := exec.Command("bash", "-c", javaCommand) //"java", "-Djava.awt.headless=true", "-jar", "plantuml.jar", "-tsvg", indir)
//	out, err := plantuml.CombinedOutput()
//	fmt.Println(string(out))
//	cleanUp := fmt.Sprintf("find %s  -type f -name '*.puml' -delete", dir)
//	return err, func() {
//		plantuml := exec.Command("bash", "-c", cleanUp) //"java", "-Djava.awt.headless=true", "-jar", "plantuml.jar", "-tsvg", indir)
//		err = plantuml.Run()
//	}
//}

// GenerateAndWriteMermaidDiagram writes a mermaid svg to file
func GenerateAndWriteMermaidDiagram(fs afero.Fs, fileName string, data string) error {
	mermaidSvg := []byte(mermaid.Execute(data) + "\n")
	var err = afero.WriteFile(fs, fileName, mermaidSvg, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

// GenerateRedoc creates a redoc html file
func GenerateAndWriteRedoc(fs afero.Fs, fileName string, specURL string) error {
	redoc := BuildRedoc(specURL)
	err := afero.WriteFile(fs, fileName, redoc, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

// CreateSequenceDiagram creates an sequence diagram and returns the sequence diagram string and any errors
func CreateSequenceDiagram(m *sysl.Module, call string) (string, error) {
	l := &cmdutils.Labeler{}
	p := &sequencediagram.SequenceDiagParam{
		AppLabeler:      l,
		EndpointLabeler: l,
		Endpoints:       []string{call},
		Title:           call,
	}
	return sequencediagram.GenerateSequenceDiag(m, p, logrus.New())
}

type Typer interface {
	GetType() *sysl.Type
}

// GetAppTypeName takes a Sysl Type and returns the appName and typeName of a param
// If the type is a primitive, the appName returned is "primitive"
func GetAppTypeName(param Typer) (appName string, typeName string) {
	ref := param.GetType().GetTypeRef().GetRef()
	appNameParts := ref.GetAppname().GetPart()
	if a, b := syslutil.GetTypeDetail(param.GetType()); a == "primitive" {
		return a, b
	}
	if len(appNameParts) > 0 {
		typeNameParts := ref.GetPath()
		if typeNameParts != nil {
			appName = appNameParts[0]
			typeName = typeNameParts[0]
		} else {
			typeName = appNameParts[0]
		}
	} else {
		typeName = ref.GetPath()[0]
	}
	return appName, typeName
}
