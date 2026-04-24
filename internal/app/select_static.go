package app

import appcards "feidex/internal/app/cards"

type selectStaticOption = appcards.SelectStaticOption

func buildSelectStaticElement(name, placeholder string, actionValue map[string]any, options []selectStaticOption, initialOption string) map[string]any {
	return appcards.BuildSelectStaticElement(name, placeholder, actionValue, options, initialOption)
}

func buildFormSelectStaticElement(name, placeholder string, options []selectStaticOption, initialOption string) map[string]any {
	return appcards.BuildFormSelectStaticElement(name, placeholder, options, initialOption)
}
