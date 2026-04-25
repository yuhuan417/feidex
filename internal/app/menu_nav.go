package app

import "strings"

func menuBreadcrumbLabels(action string) []string {
	return menuBreadcrumbLabelsForBackend(action, "")
}

func menuBreadcrumbLabelsForBackend(action, backend string) []string {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "menu.root"
	}
	labels := []string{}
	for i := 0; action != "" && i < 16; i++ {
		node, ok := menuNodes[action]
		if !ok {
			break
		}
		labels = append(labels, menuNodeLabelForBackend(action, node.Label, backend))
		action = node.Parent
	}
	for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
		labels[i], labels[j] = labels[j], labels[i]
	}
	return labels
}

func menuCardBody(action, body string) string {
	return menuCardBodyForBackend("", action, body)
}

func menuCardBodyForBackend(backend, action, body string) string {
	breadcrumbs := strings.Join(menuBreadcrumbLabelsForBackend(action, backend), " / ")
	body = strings.TrimSpace(body)
	if breadcrumbs == "" {
		return body
	}
	if body == "" {
		return "当前位置：" + breadcrumbs
	}
	return "当前位置：" + breadcrumbs + "\n\n" + body
}

func menuNodeLabelForBackend(action, label, backend string) string {
	return backendCapabilityForKind(backend).MenuNodeLabel(action, label)
}

func submenuLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "›"
	}
	return label + " ›"
}

func commandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" {
		return slash
	}
	if slash == "" {
		return label
	}
	return label + " " + slash
}

func submenuCommandLabel(label, slash string) string {
	return submenuLabel(commandLabel(label, slash))
}
