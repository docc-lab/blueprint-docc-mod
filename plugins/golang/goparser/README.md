<!-- Code generated by gomarkdoc. DO NOT EDIT -->

# goparser

```go
import "gitlab.mpi-sws.org/cld/blueprint/plugins/golang/goparser"
```

## Index

- [type ParsedField](<#ParsedField>)
  - [func \(f \*ParsedField\) Parse\(\) error](<#ParsedField.Parse>)
  - [func \(f \*ParsedField\) String\(\) string](<#ParsedField.String>)
- [type ParsedFile](<#ParsedFile>)
  - [func \(f \*ParsedFile\) LoadFuncs\(\) error](<#ParsedFile.LoadFuncs>)
  - [func \(f \*ParsedFile\) LoadImports\(\) error](<#ParsedFile.LoadImports>)
  - [func \(f \*ParsedFile\) LoadStructsAndInterfaces\(\) error](<#ParsedFile.LoadStructsAndInterfaces>)
  - [func \(f \*ParsedFile\) ResolveIdent\(name string\) gocode.TypeName](<#ParsedFile.ResolveIdent>)
  - [func \(f \*ParsedFile\) ResolveSelector\(packageShortName string, name string\) gocode.TypeName](<#ParsedFile.ResolveSelector>)
  - [func \(f \*ParsedFile\) ResolveType\(expr ast.Expr\) gocode.TypeName](<#ParsedFile.ResolveType>)
  - [func \(f \*ParsedFile\) String\(\) string](<#ParsedFile.String>)
- [type ParsedFunc](<#ParsedFunc>)
  - [func \(f \*ParsedFunc\) AsConstructor\(\) \*gocode.Constructor](<#ParsedFunc.AsConstructor>)
  - [func \(f \*ParsedFunc\) Parse\(\) error](<#ParsedFunc.Parse>)
  - [func \(f \*ParsedFunc\) String\(\) string](<#ParsedFunc.String>)
- [type ParsedImport](<#ParsedImport>)
- [type ParsedInterface](<#ParsedInterface>)
  - [func \(iface \*ParsedInterface\) ServiceInterface\(ctx blueprint.BuildContext\) \*gocode.ServiceInterface](<#ParsedInterface.ServiceInterface>)
  - [func \(iface \*ParsedInterface\) Type\(\) \*gocode.UserType](<#ParsedInterface.Type>)
- [type ParsedModule](<#ParsedModule>)
  - [func \(mod \*ParsedModule\) Load\(\) error](<#ParsedModule.Load>)
  - [func \(mod \*ParsedModule\) String\(\) string](<#ParsedModule.String>)
- [type ParsedModuleSet](<#ParsedModuleSet>)
  - [func ParseModules\(srcDirs ...string\) \(\*ParsedModuleSet, error\)](<#ParseModules>)
  - [func ParseWorkspace\(workspaceDir string\) \(\*ParsedModuleSet, error\)](<#ParseWorkspace>)
  - [func \(set \*ParsedModuleSet\) AddModule\(srcDir string\) \(\*ParsedModule, error\)](<#ParsedModuleSet.AddModule>)
  - [func \(set \*ParsedModuleSet\) GetPackage\(name string\) \*ParsedPackage](<#ParsedModuleSet.GetPackage>)
  - [func \(set \*ParsedModuleSet\) String\(\) string](<#ParsedModuleSet.String>)
- [type ParsedPackage](<#ParsedPackage>)
  - [func \(pkg \*ParsedPackage\) Load\(\) error](<#ParsedPackage.Load>)
  - [func \(pkg \*ParsedPackage\) Parse\(\) error](<#ParsedPackage.Parse>)
  - [func \(pkg \*ParsedPackage\) String\(\) string](<#ParsedPackage.String>)
- [type ParsedStruct](<#ParsedStruct>)
  - [func \(f \*ParsedStruct\) String\(\) string](<#ParsedStruct.String>)
  - [func \(struc \*ParsedStruct\) Type\(\) \*gocode.UserType](<#ParsedStruct.Type>)


<a name="ParsedField"></a>
## type ParsedField

A set of modules on the local filesystem that contain workflow spec interfaces and implementations. It is allowed for a workflow spec implementation in one package to use the interface defined in another package. However, currently, it is not possible to use workflow spec nodes whose interface or implementation comes entirely from an external module \(ie. a module that exists only as a 'require' directive of a go.mod\)

```go
type ParsedField struct {
    gocode.Variable
    Struct   *ParsedStruct
    Position int
    Ast      *ast.Field
}
```

<a name="ParsedField.Parse"></a>
### func \(\*ParsedField\) Parse

```go
func (f *ParsedField) Parse() error
```



<a name="ParsedField.String"></a>
### func \(\*ParsedField\) String

```go
func (f *ParsedField) String() string
```



<a name="ParsedFile"></a>
## type ParsedFile

A set of modules on the local filesystem that contain workflow spec interfaces and implementations. It is allowed for a workflow spec implementation in one package to use the interface defined in another package. However, currently, it is not possible to use workflow spec nodes whose interface or implementation comes entirely from an external module \(ie. a module that exists only as a 'require' directive of a go.mod\)

```go
type ParsedFile struct {
    Package          *ParsedPackage
    Name             string                   // Filename
    Path             string                   // Fully qualified path to the file
    AnonymousImports []*ParsedImport          // Import declarations that were imported with .
    NamedImports     map[string]*ParsedImport // Import declarations - map from shortname to fully qualified package import name
    Ast              *ast.File                // The AST of the file
}
```

<a name="ParsedFile.LoadFuncs"></a>
### func \(\*ParsedFile\) LoadFuncs

```go
func (f *ParsedFile) LoadFuncs() error
```

Assumes that all structs and interfaces have been loaded for the package containing the file.

Loads the names of all funcs. If the func has a receiver type, then it is saved as a method on the appropriate struct; if it does not have a receiver type, then it is saved as a package func.

This does not parse the arguments or returns of the func

<a name="ParsedFile.LoadImports"></a>
### func \(\*ParsedFile\) LoadImports

```go
func (f *ParsedFile) LoadImports() error
```



<a name="ParsedFile.LoadStructsAndInterfaces"></a>
### func \(\*ParsedFile\) LoadStructsAndInterfaces

```go
func (f *ParsedFile) LoadStructsAndInterfaces() error
```

Looks for:

- structs defined in the file
- interfaces defined in the file
- other user types defined in the file

Does not:

- look for function declarations

<a name="ParsedFile.ResolveIdent"></a>
### func \(\*ParsedFile\) ResolveIdent

```go
func (f *ParsedFile) ResolveIdent(name string) gocode.TypeName
```

An ident can be:

- a basic type, like int64, float32 etc.
- any
- a type declared locally within the file or package
- a type imported with an \`import . "package"\` decl

<a name="ParsedFile.ResolveSelector"></a>
### func \(\*ParsedFile\) ResolveSelector

```go
func (f *ParsedFile) ResolveSelector(packageShortName string, name string) gocode.TypeName
```



<a name="ParsedFile.ResolveType"></a>
### func \(\*ParsedFile\) ResolveType

```go
func (f *ParsedFile) ResolveType(expr ast.Expr) gocode.TypeName
```



<a name="ParsedFile.String"></a>
### func \(\*ParsedFile\) String

```go
func (f *ParsedFile) String() string
```



<a name="ParsedFunc"></a>
## type ParsedFunc

A set of modules on the local filesystem that contain workflow spec interfaces and implementations. It is allowed for a workflow spec implementation in one package to use the interface defined in another package. However, currently, it is not possible to use workflow spec nodes whose interface or implementation comes entirely from an external module \(ie. a module that exists only as a 'require' directive of a go.mod\)

```go
type ParsedFunc struct {
    gocode.Func
    File *ParsedFile
    Ast  *ast.FuncType
}
```

<a name="ParsedFunc.AsConstructor"></a>
### func \(\*ParsedFunc\) AsConstructor

```go
func (f *ParsedFunc) AsConstructor() *gocode.Constructor
```



<a name="ParsedFunc.Parse"></a>
### func \(\*ParsedFunc\) Parse

```go
func (f *ParsedFunc) Parse() error
```



<a name="ParsedFunc.String"></a>
### func \(\*ParsedFunc\) String

```go
func (f *ParsedFunc) String() string
```



<a name="ParsedImport"></a>
## type ParsedImport

A set of modules on the local filesystem that contain workflow spec interfaces and implementations. It is allowed for a workflow spec implementation in one package to use the interface defined in another package. However, currently, it is not possible to use workflow spec nodes whose interface or implementation comes entirely from an external module \(ie. a module that exists only as a 'require' directive of a go.mod\)

```go
type ParsedImport struct {
    File    *ParsedFile
    Package string
}
```

<a name="ParsedInterface"></a>
## type ParsedInterface

A set of modules on the local filesystem that contain workflow spec interfaces and implementations. It is allowed for a workflow spec implementation in one package to use the interface defined in another package. However, currently, it is not possible to use workflow spec nodes whose interface or implementation comes entirely from an external module \(ie. a module that exists only as a 'require' directive of a go.mod\)

```go
type ParsedInterface struct {
    File    *ParsedFile
    Ast     *ast.InterfaceType
    Name    string
    Methods map[string]*ParsedFunc
}
```

<a name="ParsedInterface.ServiceInterface"></a>
### func \(\*ParsedInterface\) ServiceInterface

```go
func (iface *ParsedInterface) ServiceInterface(ctx blueprint.BuildContext) *gocode.ServiceInterface
```



<a name="ParsedInterface.Type"></a>
### func \(\*ParsedInterface\) Type

```go
func (iface *ParsedInterface) Type() *gocode.UserType
```



<a name="ParsedModule"></a>
## type ParsedModule

A set of modules on the local filesystem that contain workflow spec interfaces and implementations. It is allowed for a workflow spec implementation in one package to use the interface defined in another package. However, currently, it is not possible to use workflow spec nodes whose interface or implementation comes entirely from an external module \(ie. a module that exists only as a 'require' directive of a go.mod\)

```go
type ParsedModule struct {
    ModuleSet *ParsedModuleSet
    Name      string                    // Fully qualified name of the module
    Version   string                    // Version of the module
    SrcDir    string                    // Fully qualified location of the module on the filesystem
    Modfile   *modfile.File             // The modfile File struct is sufficiently simple that we just use it directly
    Packages  map[string]*ParsedPackage // Map from fully qualified package name to ParsedPackage
}
```

<a name="ParsedModule.Load"></a>
### func \(\*ParsedModule\) Load

```go
func (mod *ParsedModule) Load() error
```



<a name="ParsedModule.String"></a>
### func \(\*ParsedModule\) String

```go
func (mod *ParsedModule) String() string
```



<a name="ParsedModuleSet"></a>
## type ParsedModuleSet

A set of modules on the local filesystem that contain workflow spec interfaces and implementations. It is allowed for a workflow spec implementation in one package to use the interface defined in another package. However, currently, it is not possible to use workflow spec nodes whose interface or implementation comes entirely from an external module \(ie. a module that exists only as a 'require' directive of a go.mod\)

```go
type ParsedModuleSet struct {
    Modules map[string]*ParsedModule // Map from FQ module name to module object
}
```

<a name="ParseModules"></a>
### func ParseModules

```go
func ParseModules(srcDirs ...string) (*ParsedModuleSet, error)
```

Parse the specified module directories

<a name="ParseWorkspace"></a>
### func ParseWorkspace

```go
func ParseWorkspace(workspaceDir string) (*ParsedModuleSet, error)
```

Parse all modules in the specified directory

<a name="ParsedModuleSet.AddModule"></a>
### func \(\*ParsedModuleSet\) AddModule

```go
func (set *ParsedModuleSet) AddModule(srcDir string) (*ParsedModule, error)
```



<a name="ParsedModuleSet.GetPackage"></a>
### func \(\*ParsedModuleSet\) GetPackage

```go
func (set *ParsedModuleSet) GetPackage(name string) *ParsedPackage
```



<a name="ParsedModuleSet.String"></a>
### func \(\*ParsedModuleSet\) String

```go
func (set *ParsedModuleSet) String() string
```



<a name="ParsedPackage"></a>
## type ParsedPackage

A set of modules on the local filesystem that contain workflow spec interfaces and implementations. It is allowed for a workflow spec implementation in one package to use the interface defined in another package. However, currently, it is not possible to use workflow spec nodes whose interface or implementation comes entirely from an external module \(ie. a module that exists only as a 'require' directive of a go.mod\)

```go
type ParsedPackage struct {
    Module        *ParsedModule
    Name          string                      // Fully qualified name of the package including module name
    ShortName     string                      // Shortname of the package (ie, the name used in an import statement)
    PackageDir    string                      // Subdirectory within the module containing the package
    SrcDir        string                      // Fully qualified location of the package on the filesystem
    Files         map[string]*ParsedFile      // Map from filename to ParsedFile
    Ast           *ast.Package                // The AST of the package
    DeclaredTypes map[string]gocode.UserType  // Types declared within this package
    Structs       map[string]*ParsedStruct    // Structs parsed from this package
    Interfaces    map[string]*ParsedInterface // Interfaces parsed from this package
    Funcs         map[string]*ParsedFunc      // Functions parsed from this package (does not include funcs with receiver types)
}
```

<a name="ParsedPackage.Load"></a>
### func \(\*ParsedPackage\) Load

```go
func (pkg *ParsedPackage) Load() error
```



<a name="ParsedPackage.Parse"></a>
### func \(\*ParsedPackage\) Parse

```go
func (pkg *ParsedPackage) Parse() error
```



<a name="ParsedPackage.String"></a>
### func \(\*ParsedPackage\) String

```go
func (pkg *ParsedPackage) String() string
```



<a name="ParsedStruct"></a>
## type ParsedStruct

A set of modules on the local filesystem that contain workflow spec interfaces and implementations. It is allowed for a workflow spec implementation in one package to use the interface defined in another package. However, currently, it is not possible to use workflow spec nodes whose interface or implementation comes entirely from an external module \(ie. a module that exists only as a 'require' directive of a go.mod\)

```go
type ParsedStruct struct {
    File            *ParsedFile
    Ast             *ast.StructType
    Name            string
    Methods         map[string]*ParsedFunc  // Methods declared directly on this struct, does not include promoted methods (not implemented yet)
    FieldsList      []*ParsedField          // All fields in the order that they are declared
    Fields          map[string]*ParsedField // Named fields declared in this struct only, does not include promoted fields (not implemented yet)
    PromotedField   *ParsedField            // If there is a promoted field, stored here
    AnonymousFields []*ParsedField          // Subsequent anonymous fields
}
```

<a name="ParsedStruct.String"></a>
### func \(\*ParsedStruct\) String

```go
func (f *ParsedStruct) String() string
```



<a name="ParsedStruct.Type"></a>
### func \(\*ParsedStruct\) Type

```go
func (struc *ParsedStruct) Type() *gocode.UserType
```



Generated by [gomarkdoc](<https://github.com/princjef/gomarkdoc>)