package tests

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/fatih/set"
	"github.com/stretchr/testify/assert"

	"github.com/elastic/apm-server/processor"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/template"
)

func TestEventAttrsDocumentedInFields(t *testing.T, fieldPaths []string, fn processor.NewProcessor) {
	assert := assert.New(t)
	fieldNames, err := fetchFlattenedFieldNames(fieldPaths, addAllFields)
	disabledFieldNames, err := fetchFlattenedFieldNames(fieldPaths, addOnlyDisabledFields)
	undocumentedFieldNames := set.New(
		"processor",
		//dynamically indexed:
		"context.tags.organization_uuid",
		//known not-indexed fields:
		"context.custom",
		"context.request.headers",
		"context.request.cookies",
		"context.request.socket",
		"context.request.env",
		"context.request.body",
		"context.response.headers",
		"context.app.argv",
		"error.exception.attributes",
		"error.exception.stacktrace",
		"error.log.stacktrace",
		"trace.stacktrace",
		"context.sql",
	)
	blacklistedFieldNames := set.Union(disabledFieldNames, undocumentedFieldNames).(*set.Set)

	eventNames, err := fetchEventNames(fn, blacklistedFieldNames)
	assert.NoError(err)

	undocumentedNames := set.Difference(eventNames, fieldNames, blacklistedFieldNames)
	assert.Equal(0, undocumentedNames.Size(), fmt.Sprintf("Event attributes not documented in fields.yml: %v", undocumentedNames))
}

func TestDocumentedFieldsInEvent(t *testing.T, fieldPaths []string, fn processor.NewProcessor, exceptions *set.Set) {
	assert := assert.New(t)
	fieldNames, err := fetchFlattenedFieldNames(fieldPaths, addAllFields)
	assert.NoError(err)

	eventNames, err := fetchEventNames(fn, set.New())
	assert.NoError(err)

	unusedNames := set.Difference(fieldNames, eventNames, exceptions)
	assert.Equal(0, unusedNames.Size(), fmt.Sprintf("Documented Fields missing in event: %v", unusedNames))
}

func fetchEventNames(fn processor.NewProcessor, blacklisted *set.Set) (*set.Set, error) {
	p := fn()
	data, _ := LoadValidData(p.Name())
	err := p.Validate(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	events := p.Transform()

	eventNames := set.New()
	for _, event := range events {
		for k, _ := range event.Fields {
			if k == "@timestamp" {
				continue
			}
			e := event.Fields[k]
			flattenMapStr(e, k, blacklisted, eventNames)
		}
	}
	return eventNames, nil
}

func flattenMapStr(m interface{}, prefix string, keysBlacklist *set.Set, flattened *set.Set) {
	if commonMapStr, ok := m.(common.MapStr); ok {
		for k, v := range commonMapStr {
			flattenMapStrStr(k, v, prefix, keysBlacklist, flattened)
		}
	} else if mapStr, ok := m.(map[string]interface{}); ok {
		for k, v := range mapStr {
			flattenMapStrStr(k, v, prefix, keysBlacklist, flattened)
		}
	}
	if prefix != "" && !isBlacklistedKey(keysBlacklist, prefix) {
		flattened.Add(prefix)
	}
}

func flattenMapStrStr(k string, v interface{}, prefix string, keysBlacklist *set.Set, flattened *set.Set) {
	flattenedKey := StrConcat(prefix, k, ".")
	if !isBlacklistedKey(keysBlacklist, flattenedKey) {
		flattened.Add(flattenedKey)
	}
	_, okCommonMapStr := v.(common.MapStr)
	_, okMapStr := v.(map[string]interface{})
	if okCommonMapStr || okMapStr {
		flattenMapStr(v, flattenedKey, keysBlacklist, flattened)
	}
}

func isBlacklistedKey(keysBlacklist *set.Set, key string) bool {
	for _, disabledKey := range keysBlacklist.List() {
		if strings.HasPrefix(key, disabledKey.(string)) {
			return true

		}
	}
	return false
}

func fetchFlattenedFieldNames(paths []string, addFn addField) (*set.Set, error) {
	fields := set.New()
	for _, path := range paths {
		f, err := loadFields(path)
		if err != nil {
			return nil, err
		}
		flattenFieldNames(f, "", addFn, fields)
	}
	return fields, nil
}

func flattenFieldNames(fields []template.Field, prefix string, addFn addField, flattened *set.Set) {
	for _, field := range fields {
		flattenedKey := StrConcat(prefix, field.Name, ".")
		if addFn(field) {
			flattened.Add(flattenedKey)
		}
		flattenFieldNames(field.Fields, flattenedKey, addFn, flattened)
	}
}

func loadFields(yamlPath string) ([]template.Field, error) {
	fields := []template.Field{}

	yaml, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}
	cfg, err := common.NewConfigWithYAML(yaml, "")
	if err != nil {
		return nil, err
	}
	err = cfg.Unpack(&fields)
	if err != nil {
		return nil, err
	}
	return fields, err
}

type addField func(f template.Field) bool

func addAllFields(f template.Field) bool {
	return shouldAddField(f, false)
}

func addOnlyDisabledFields(f template.Field) bool {
	return shouldAddField(f, true)
}

func addKeywordFields(f template.Field) bool {
	if f.Type == "keyword" || f.ObjectType == "keyword" {
		return true
	} else if len(f.MultiFields) > 0 {
		for _, mf := range f.MultiFields {
			if mf.Type == "keyword" {
				return true
			}
		}
	}
	return false
}

func shouldAddField(f template.Field, onlyDisabled bool) bool {
	if f.Name == "" {
		return false
	}
	if !onlyDisabled {
		return true
	}
	if f.Enabled != nil && *f.Enabled == false {
		return true
	}
	return false
}
