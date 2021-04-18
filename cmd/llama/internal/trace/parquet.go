// Copyright 2020 Nelson Elhage
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package trace

import (
	"fmt"
	"math"
	"os"
	"strings"

	goparquet "github.com/fraugster/parquet-go"
	"github.com/fraugster/parquet-go/parquet"
	"github.com/fraugster/parquet-go/parquetschema"
	"github.com/nelhage/llama/tracing"
)

type fieldType int

const (
	type_invalid fieldType = iota
	type_string
	type_int64
	type_float64
	type_boolean
)

func collectFields(spans []tracing.Span) (map[string]fieldType, []*parquetschema.ColumnDefinition) {
	fields := make(map[string]fieldType)
	for _, span := range spans {
		for k, v := range span.Fields {
			var ty fieldType
			switch t := v.(type) {
			case string:
				ty = type_string
			case int, int32, uint32, int64, uint64:
				ty = type_int64
			case float64:
				if math.Round(t) == t {
					ty = type_int64
				} else {
					ty = type_float64
				}
				ty = type_float64
			case bool:
				ty = type_boolean
			default:
			}
			prev, ok := fields[k]
			if ok {
				if prev != ty {
					if prev == type_int64 && ty == type_float64 ||
						prev == type_float64 && ty == type_int64 {
						fields[k] = type_float64
					} else {
						fields[k] = type_invalid
					}
					fields[k] = type_invalid
				}
			} else {
				fields[k] = ty
			}
		}
	}
	var out []*parquetschema.ColumnDefinition
	for k, v := range fields {
		if v == type_invalid {
			continue
		}
		var col parquetschema.ColumnDefinition
		col.SchemaElement = &parquet.SchemaElement{
			Name: strings.ToLower(k),
		}
		col.SchemaElement.RepetitionType = parquet.FieldRepetitionTypePtr(parquet.FieldRepetitionType_OPTIONAL)
		switch v {
		case type_string:
			col.SchemaElement.Type = parquet.TypePtr(parquet.Type_BYTE_ARRAY)
			col.SchemaElement.ConvertedType = parquet.ConvertedTypePtr(parquet.ConvertedType_UTF8)
		case type_int64:
			col.SchemaElement.Type = parquet.TypePtr(parquet.Type_INT64)
		case type_float64:
			col.SchemaElement.Type = parquet.TypePtr(parquet.Type_DOUBLE)
		case type_boolean:
			col.SchemaElement.Type = parquet.TypePtr(parquet.Type_BOOLEAN)
		}
		out = append(out, &col)
	}
	return fields, out
}

func int32ptr(i int32) *int32 {
	return &i
}

func collectParquetSchema(spans []tracing.Span) (map[string]fieldType, *parquetschema.SchemaDefinition) {
	fields, fieldCols := collectFields(spans)
	columns := []*parquetschema.ColumnDefinition{
		{
			SchemaElement: &parquet.SchemaElement{
				Name:           "trace_id",
				Type:           parquet.TypePtr(parquet.Type_BYTE_ARRAY),
				ConvertedType:  parquet.ConvertedTypePtr(parquet.ConvertedType_UTF8),
				RepetitionType: parquet.FieldRepetitionTypePtr(parquet.FieldRepetitionType_REQUIRED),
			},
		},
		{
			SchemaElement: &parquet.SchemaElement{
				Name:           "span_id",
				Type:           parquet.TypePtr(parquet.Type_BYTE_ARRAY),
				ConvertedType:  parquet.ConvertedTypePtr(parquet.ConvertedType_UTF8),
				RepetitionType: parquet.FieldRepetitionTypePtr(parquet.FieldRepetitionType_REQUIRED),
			},
		},
		{
			SchemaElement: &parquet.SchemaElement{
				Name:           "parent_id",
				Type:           parquet.TypePtr(parquet.Type_BYTE_ARRAY),
				ConvertedType:  parquet.ConvertedTypePtr(parquet.ConvertedType_UTF8),
				RepetitionType: parquet.FieldRepetitionTypePtr(parquet.FieldRepetitionType_OPTIONAL),
			},
		},
		{
			SchemaElement: &parquet.SchemaElement{
				Name:           "name",
				Type:           parquet.TypePtr(parquet.Type_BYTE_ARRAY),
				ConvertedType:  parquet.ConvertedTypePtr(parquet.ConvertedType_UTF8),
				RepetitionType: parquet.FieldRepetitionTypePtr(parquet.FieldRepetitionType_REQUIRED),
			},
		},
		{
			SchemaElement: &parquet.SchemaElement{
				Name:           "path",
				Type:           parquet.TypePtr(parquet.Type_BYTE_ARRAY),
				ConvertedType:  parquet.ConvertedTypePtr(parquet.ConvertedType_UTF8),
				RepetitionType: parquet.FieldRepetitionTypePtr(parquet.FieldRepetitionType_REQUIRED),
			},
		},
		{
			SchemaElement: &parquet.SchemaElement{
				Name:          "start",
				Type:          parquet.TypePtr(parquet.Type_INT64),
				ConvertedType: parquet.ConvertedTypePtr(parquet.ConvertedType_TIMESTAMP_MICROS),
			},
		},
		{
			SchemaElement: &parquet.SchemaElement{
				Name: "duration_us",
				Type: parquet.TypePtr(parquet.Type_INT64),
			},
		},
	}
	columns = append(columns, fieldCols...)

	rootCol := &parquetschema.ColumnDefinition{
		SchemaElement: &parquet.SchemaElement{
			Name: "spans",
		},
		Children: columns,
	}
	root := parquetschema.SchemaDefinitionFromColumnDefinition(rootCol)
	return fields, root
}

func writeParquetTree(fw *goparquet.FileWriter, fieldTypes map[string]fieldType, tree *TraceTree) error {
	global := collectGlobal(tree)

	var walk func(t *TraceTree, path string) error
	walk = func(t *TraceTree, path string) error {
		if path == "" {
			path = t.span.Name
		} else {
			path = fmt.Sprintf("%s>%s", path, t.span.Name)
		}
		columns := make(map[string]interface{})

		columns["trace_id"] = []byte(t.span.TraceId)
		columns["span_id"] = []byte(t.span.SpanId)
		if t.span.ParentId != "" {
			columns["parent_id"] = []byte(t.span.ParentId)
		}
		columns["name"] = []byte(t.span.Name)
		columns["path"] = []byte(path)
		columns["start"] = t.span.Start.UnixNano() / 1000
		columns["duration_us"] = t.span.Duration.Microseconds()

		for k, ty := range fieldTypes {
			if ty == type_invalid {
				continue
			}
			v := global[k]
			if v == nil {
				v = t.span.Fields[k]
			}
			if v == nil {
				continue
			}
			switch ty {
			case type_string:
				columns[k] = []byte(v.(string))
			case type_int64:
				switch t := v.(type) {
				case int:
					columns[k] = int64(t)
				case int32:
					columns[k] = int64(t)
				case uint32:
					columns[k] = int64(t)
				case int64:
					columns[k] = int64(t)
				case uint64:
					columns[k] = int64(t)
				case float64:
					columns[k] = int64(t)
				}
			case type_float64:
				columns[k] = v
			case type_boolean:
				columns[k] = v
			}
		}

		if err := fw.AddData(columns); err != nil {
			return err
		}
		for _, child := range t.children {
			if err := walk(child, path); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(tree, "")
}

func (c *TraceCommand) WriteParquet(spans []tracing.Span, trees []*TraceTree) error {
	fh, err := os.OpenFile(c.parquet, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer fh.Close()

	fieldTypes, schema := collectParquetSchema(spans)

	fw := goparquet.NewFileWriter(fh,
		// TODO: write our own zstd compressor
		goparquet.WithCompressionCodec(parquet.CompressionCodec_SNAPPY),
		goparquet.WithSchemaDefinition(schema),
		goparquet.WithCreator("llama-trace"),
		goparquet.WithMaxRowGroupSize(2*1024*1024),
	)

	for _, tree := range trees {
		if err := writeParquetTree(fw, fieldTypes, tree); err != nil {
			return err
		}
	}

	return fw.Close()
}
