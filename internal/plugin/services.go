package plugin

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

type ServiceCatalog struct {
	order   []string
	names   map[string]string
	aliases map[string]string
}

func NewServiceCatalog(order []string, names, aliases map[string]string) ServiceCatalog {
	catalog := ServiceCatalog{order: slices.Clone(order), names: maps.Clone(names), aliases: maps.Clone(aliases)}
	if catalog.aliases == nil {
		catalog.aliases = map[string]string{}
	}
	for key := range names {
		catalog.aliases[key] = key
	}
	return catalog
}

func (catalog ServiceCatalog) clone() ServiceCatalog {
	return NewServiceCatalog(catalog.order, catalog.names, catalog.aliases)
}

func (catalog ServiceCatalog) validate() error {
	if strings.TrimSpace(catalog.names["all"]) == "" || len(catalog.order) == 0 {
		return fmt.Errorf("missing service catalog")
	}
	seen := map[string]bool{"all": true}
	for _, service := range catalog.order {
		if service == "" || seen[service] || strings.TrimSpace(catalog.names[service]) == "" {
			return fmt.Errorf("invalid service %q", service)
		}
		seen[service] = true
	}
	for alias, service := range catalog.aliases {
		if strings.TrimSpace(alias) == "" || !seen[service] {
			return fmt.Errorf("invalid service alias %q", alias)
		}
	}
	return nil
}

func (catalog ServiceCatalog) Normalize(value string) string {
	value = strings.TrimSpace(value)
	if service, ok := catalog.aliases[value]; ok {
		return service
	}
	return catalog.aliases[strings.ToLower(value)]
}

func (catalog ServiceCatalog) NormalizeAll(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		service := catalog.Normalize(value)
		if service == "" || seen[service] {
			continue
		}
		if service == "all" {
			return []string{"all"}
		}
		seen[service] = true
		result = append(result, service)
	}
	if len(result) == 0 || len(result) == len(catalog.order) {
		return []string{"all"}
	}
	return result
}

func (catalog ServiceCatalog) ParseArgs(args []string) ([]string, string, bool) {
	values := make([]string, 0, len(args))
	for _, value := range args {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return nil, "", false
	}
	selected := []string{"all"}
	if service := catalog.Normalize(values[0]); service != "" {
		selected, values = []string{service}, values[1:]
	}
	query := strings.TrimSpace(strings.Join(values, " "))
	return catalog.NormalizeAll(selected), query, query != ""
}

func (catalog ServiceCatalog) Merge(existing, incoming []string) []string {
	if ContainsService(existing, "all") || ContainsService(incoming, "all") {
		return []string{"all"}
	}
	return catalog.NormalizeAll(append(slices.Clone(existing), incoming...))
}

func (catalog ServiceCatalog) Remove(existing, removing []string) []string {
	if ContainsService(removing, "all") {
		return nil
	}
	current := catalog.NormalizeAll(existing)
	if ContainsService(current, "all") {
		current = slices.Clone(catalog.order)
	}
	remaining := make([]string, 0, len(current))
	for _, service := range current {
		if !ContainsService(removing, service) {
			remaining = append(remaining, service)
		}
	}
	return remaining
}

func ContainsService(values []string, target string) bool { return slices.Contains(values, target) }

func (catalog ServiceCatalog) Enabled(item Subscription, service string) bool {
	values := catalog.NormalizeAll(item.Services)
	return ContainsService(values, "all") || ContainsService(values, service)
}

func (catalog ServiceCatalog) Labels(values []string) []string {
	values = catalog.NormalizeAll(values)
	labels := make([]string, 0, len(values))
	for _, value := range values {
		labels = append(labels, catalog.names[value])
	}
	return labels
}

func (catalog ServiceCatalog) Text(values []string) string {
	return strings.Join(catalog.Labels(values), "、")
}

func (catalog ServiceCatalog) Label(service string) string { return catalog.names[service] }

func (handler *Handler) ParseSubscriptionArgs(args []string, platform string) ([]string, string, bool) {
	return handler.byID[platform].Services.ParseArgs(args)
}

func (handler *Handler) SubjectIDFromInput(platform, value string) string {
	if definition, ok := handler.byID[platform]; ok {
		return definition.ParseSubject(value)
	}
	return ""
}
