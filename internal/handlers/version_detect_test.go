package handlers

import (
    "testing"
    pppkg "modsentinel/internal/pufferpanel"
)

func TestDetectGameVersion_KeyPatternsAndOptions(t *testing.T) {
    cases := []struct{
        name string
        def  *pppkg.ServerDefinition
        data *pppkg.ServerData
        wantKey string
        wantVal string
        ok   bool
    }{
        {
            name: "MC_VERSION with options",
            def: &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{
                "MC_VERSION": {Display: "Minecraft Version", Options: []string{"1.20.1","1.21"}},
            }},
            data: &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{
                "MC_VERSION": {Value: "1.20.1"},
            }},
            wantKey: "MC_VERSION", wantVal: "1.20.1", ok: true,
        },
        {
            name: "MINECRAFT_VERSION free text",
            def: &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{
                "MINECRAFT_VERSION": {Display: "Minecraft", Desc: "Version"},
            }},
            data: &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{
                "MINECRAFT_VERSION": {Value: "1.21"},
            }},
            wantKey: "MINECRAFT_VERSION", wantVal: "1.21", ok: true,
        },
        {
            name: "VERSION with suffix",
            def: &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{
                "VERSION": {Display: "Version"},
            }},
            data: &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{
                "VERSION": {Value: "1.20.1-fabric"},
            }},
            wantKey: "VERSION", wantVal: "1.20.1-fabric", ok: true,
        },
        {
            name: "Prefer with matching options",
            def: &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{
                "MC_VERSION": {Display: "Version"},
                "GAME_VERSION": {Display: "Version", Options: []string{"1.20.1","1.19.4"}},
            }},
            data: &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{
                "MC_VERSION": {Value: "1.20.1"},
                "GAME_VERSION": {Value: "1.20.1"},
            }},
            wantKey: "GAME_VERSION", wantVal: "1.20.1", ok: true,
        },
        {
            name: "Reject non-version value",
            def: &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{
                "MINECRAFT_VERSION": {Display: "Version"},
            }},
            data: &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{
                "MINECRAFT_VERSION": {Value: "latest"},
            }},
            ok: false,
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            k, v, ok := detectGameVersion(tc.def, tc.data)
            if ok != tc.ok { t.Fatalf("ok=%v want %v (k=%q v=%q)", ok, tc.ok, k, v) }
            if !ok { return }
            if k != tc.wantKey || v != tc.wantVal {
                t.Fatalf("got %q %q; want %q %q", k, v, tc.wantKey, tc.wantVal)
            }
        })
    }
}

