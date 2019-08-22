package opentsdbhttp

import (
	"fmt"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/bytesutil"
	"github.com/valyala/fastjson"
	"github.com/valyala/fastjson/fastfloat"
)

// Rows contains parsed OpenTSDB rows.
type Rows struct {
	Rows []Row

	tagsPool []Tag
}

// Reset resets rs.
func (rs *Rows) Reset() {
	// Release references to objects, so they can be GC'ed.
	for i := range rs.Rows {
		rs.Rows[i].reset()
	}
	rs.Rows = rs.Rows[:0]

	for i := range rs.tagsPool {
		rs.tagsPool[i].reset()
	}
	rs.tagsPool = rs.tagsPool[:0]
}

// Unmarshal unmarshals OpenTSDB rows from av.
//
// See http://opentsdb.net/docs/build/html/api_http/put.html
//
// s must be unchanged until rs is in use.
func (rs *Rows) Unmarshal(av *fastjson.Value) error {
	var err error
	rs.Rows, rs.tagsPool, err = unmarshalRows(rs.Rows[:0], av, rs.tagsPool[:0])
	return err
}

// Row is a single OpenTSDB row.
type Row struct {
	Metric    string
	Tags      []Tag
	Value     float64
	Timestamp int64
}

func (r *Row) reset() {
	r.Metric = ""
	r.Tags = nil
	r.Value = 0
	r.Timestamp = 0
}

func (r *Row) unmarshal(o *fastjson.Value, tagsPool []Tag) ([]Tag, error) {
	r.reset()
	m := o.GetStringBytes("metric")
	if m == nil {
		return tagsPool, fmt.Errorf("missing `metric` in %s", o)
	}
	r.Metric = bytesutil.ToUnsafeString(m)

	rawTs := o.Get("timestamp")
	if rawTs != nil {
		ts, err := rawTs.Int64()
		if err != nil {
			return tagsPool, fmt.Errorf("invalid `timestamp` in %s: %s", o, err)
		}
		r.Timestamp = int64(ts)
	} else {
		// Allow missing timestamp. It is automatically populated
		// with the current time in this case.
		r.Timestamp = 0
	}

	rawV := o.Get("value")
	if rawV == nil {
		return tagsPool, fmt.Errorf("missing `value` in %s", o)
	}
	v, err := getValue(rawV)
	if err != nil {
		return tagsPool, fmt.Errorf("invalid `value` in %s: %s", o, err)
	}
	r.Value = v

	vt := o.Get("tags")
	if vt == nil {
		// Allow empty tags.
		return tagsPool, nil
	}
	rawTags, err := vt.Object()
	if err != nil {
		return tagsPool, fmt.Errorf("invalid `tags` in %s: %s", o, err)
	}

	tagsStart := len(tagsPool)
	tagsPool, err = unmarshalTags(tagsPool, rawTags)
	if err != nil {
		return tagsPool, fmt.Errorf("cannot parse tags %s: %s", rawTags, err)
	}
	tags := tagsPool[tagsStart:]
	r.Tags = tags[:len(tags):len(tags)]
	return tagsPool, nil
}

func getValue(v *fastjson.Value) (float64, error) {
	switch v.Type() {
	case fastjson.TypeNumber:
		return v.Float64()
	case fastjson.TypeString:
		vStr, _ := v.StringBytes()
		vFloat := fastfloat.ParseBestEffort(bytesutil.ToUnsafeString(vStr))
		if vFloat == 0 && string(vStr) != "0" && string(vStr) != "0.0" {
			return 0, fmt.Errorf("invalid float64 value: %q", vStr)
		}
		return vFloat, nil
	default:
		return 0, fmt.Errorf("value doesn't contain float64; it contains %s", v.Type())
	}
}

func unmarshalRows(dst []Row, av *fastjson.Value, tagsPool []Tag) ([]Row, []Tag, error) {
	switch av.Type() {
	case fastjson.TypeObject:
		return unmarshalRow(dst, av, tagsPool)
	case fastjson.TypeArray:
		a, _ := av.Array()
		for i, o := range a {
			var err error
			dst, tagsPool, err = unmarshalRow(dst, o, tagsPool)
			if err != nil {
				return dst, tagsPool, fmt.Errorf("cannot unmarshal %d object out of %d objects: %s", i, len(a), err)
			}
		}
		return dst, tagsPool, nil
	default:
		return dst, tagsPool, fmt.Errorf("OpenTSDB body must be either object or array; got %s; body=%s", av.Type(), av)
	}
}

func unmarshalRow(dst []Row, o *fastjson.Value, tagsPool []Tag) ([]Row, []Tag, error) {
	if cap(dst) > len(dst) {
		dst = dst[:len(dst)+1]
	} else {
		dst = append(dst, Row{})
	}
	r := &dst[len(dst)-1]
	var err error
	tagsPool, err = r.unmarshal(o, tagsPool)
	if err != nil {
		return dst, tagsPool, fmt.Errorf("cannot unmarshal OpenTSDB object %s: %s", o, err)
	}
	return dst, tagsPool, nil
}

func unmarshalTags(dst []Tag, o *fastjson.Object) ([]Tag, error) {
	var err error
	o.Visit(func(k []byte, v *fastjson.Value) {
		if v.Type() != fastjson.TypeString {
			err = fmt.Errorf("tag value must be string; got %s; value=%s", v.Type(), v)
			return
		}
		vStr, _ := v.StringBytes()
		if len(vStr) == 0 {
			// Skip empty tags
			return
		}
		if cap(dst) > len(dst) {
			dst = dst[:len(dst)+1]
		} else {
			dst = append(dst, Tag{})
		}
		tag := &dst[len(dst)-1]
		tag.Key = bytesutil.ToUnsafeString(k)
		tag.Value = bytesutil.ToUnsafeString(vStr)
	})
	return dst, err
}

// Tag is an OpenTSDB tag.
type Tag struct {
	Key   string
	Value string
}

func (t *Tag) reset() {
	t.Key = ""
	t.Value = ""
}
