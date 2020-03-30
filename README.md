# syslcatalog

A markdown + Diagram generator for sysl specifications

## How to use
1. Run 

```bash
syslcatalog -o <outputdir> <input.sysl>
```

2. That's it (basically!)

## Example
In [demo/README.md](demo/README.md) we have an example with a couple of interesting parts:
1. There needs to be a "project" the same name as the filename:

so as this example is called "simple2.sysl" there needs to be a project "simple2":
- This these applications to our integration diagram.
```
simple2[~ignore]:
    _:
        MobileApp
        Server
        MegaDatabase
```

2. `@package` attribute must be specified:
 - This will create a markdown page for "ApplicationPackage" as seen in [demo/docs/ApplicationPackage/README.md](demo/docs/ApplicationPackage/README.md)
 Currently the package name is not inferred from the application name, so this needs to be added
 ```
MobileApp:
    @package = "ApplicationPackage"
    Login(input <: Server.Request):
        Server <- Authenticate
        return ok <: MegaDatabase.Empty
```

3. Application names need to be prefixed to parameter types:
- This is just a bug that I found
 ```diff
MobileApp:
    @package = "ApplicationPackage"
+    Login(input <: Server.Request):
-    Login(input <: Request):
        Server <- Authenticate
        return ok <: MegaDatabase.Empty
```