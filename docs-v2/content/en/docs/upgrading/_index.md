---
title: "Upgrading from Skaffold v1.*.* to Skaffold v2.*.*"
linkTitle: "Upgrading from Skaffold v1.*.* to Skaffold v2.*.*"
weight: 10
aliases: [/docs/upgrading-to-v2]
---

Upgrading from skaffold `v1.*.*` to skaffold `v2.*.*` does not require any additional usage or skaffold.yaml changes for most common use cases.  Skaffold v2 has the same CLI commands as v1 and .. Currently there are no known regressions for skaffold v1 -> v2 but areas that might .. include ... 

skaffold v2.0.0-beta1 has backwards compatibility with ...  If you wish to update your skaffold.yaml file to latest apiVersion - v3alpha1 you can use the built in `skaffold fix` command which will print out the updated skaffold.yaml which then you can then save and use as desired.  Old patterns of usage for example
- `render` + `apply`
- `dev`
- `debug`

are all still supported

For a list of the major changes & new feature skaffold v2 brings, see ...

- upgrading skaffold.yaml files to new schema version
- different commands to use?
- any known incompatibility



## Build Config Initialization