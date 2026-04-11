package app

import (
	"fmt"

	"feidex/internal/feishu"
)

type localCommandSpec struct {
	Names   []string
	IsLocal func(fields []string) bool
	Handle  func(a *App, msg *feishu.InboundMessage, args []string) error
}

var localCommandSpecs = []localCommandSpec{
	{
		Names: []string{"/menu"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
			return a.sendCommandMenu(msg)
		},
	},
	{
		Names: []string{"/help"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandHelp(msg, args)
		},
	},
	{
		Names: []string{"/history"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandHistory(msg, args)
		},
	},
	{
		Names: []string{"/usage"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandUsage(msg, args)
		},
	},
	{
		Names: []string{"/model"},
		IsLocal: func(fields []string) bool {
			return len(fields) == 1
		},
		Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
			return a.commandModel(msg)
		},
	},
	{
		Names: []string{"/quiet"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandQuiet(msg, args)
		},
	},
	{
		Names: []string{"/debug"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandDebug(msg, args)
		},
	},
	{
		Names: []string{"/fast"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandFast(msg, args)
		},
	},
	{
		Names: []string{"/download"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandDownload(msg, args)
		},
	},
	{
		Names: []string{"/compact"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandCompact(msg, args)
		},
	},
	{
		Names: []string{"/fork"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandFork(msg, args)
		},
	},
	{
		Names: []string{"/new"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
			return a.commandNew(msg)
		},
	},
	{
		Names: []string{"/thread"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandThread(msg, args)
		},
	},
	{
		Names: []string{"/threads"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("usage: /threads")
			}
			return a.commandThread(msg, []string{"list"})
		},
	},
	{
		Names: []string{"/interrupt", "/stop"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
			return a.commandInterrupt(msg)
		},
	},
	{
		Names: []string{"/status"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
			return a.commandStatus(msg)
		},
	},
	{
		Names: []string{"/upgrade"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandUpgrade(msg, args)
		},
	},
	{
		Names: []string{"/workspace"},
		IsLocal: func([]string) bool {
			return true
		},
		Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
			return a.commandWorkspace(msg, args)
		},
	},
}

func findLocalCommandSpec(name string) *localCommandSpec {
	for i := range localCommandSpecs {
		spec := &localCommandSpecs[i]
		for _, candidate := range spec.Names {
			if candidate == name {
				return spec
			}
		}
	}
	return nil
}
