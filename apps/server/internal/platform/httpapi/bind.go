package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
)

// MaxBodyBytes is the default request body limit, forma2's server action limit (bodySizeLimit "2mb").
const MaxBodyBytes int64 = 2 << 20

var registerJSONNames sync.Once

// validate runs the `binding` struct tags (go-playground/validator, through gin's validator) and reports failures by JSON field name.
func validate(dst any) error {
	registerJSONNames.Do(func() {
		if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
			v.RegisterTagNameFunc(jsonName)
		}
	})
	err := binding.Validator.ValidateStruct(dst)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return err
	}
	fields := make([]apperr.FieldError, 0, len(verrs))
	for _, fe := range verrs {
		fields = append(fields, apperr.FieldError{Field: fieldPath(fe), Rule: fe.Tag(), Param: fe.Param()})
	}
	return apperr.Validation(fields)
}

func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	switch name {
	case "-":
		return ""
	case "":
		return f.Name
	}
	return name
}

// fieldPath drops the top-level struct name from the validator's namespace: "signupRequest.email" becomes "email", nested fields keep their path ("owner.name").
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if _, rest, ok := strings.Cut(ns, "."); ok {
		return rest
	}
	return ns
}

// BindJSON decodes a JSON request body of at most MaxBodyBytes into dst and validates it. Unknown fields are ignored, as zod's object schemas strip them. Every failure is an *apperr.Error the handler passes straight to Fail.
func BindJSON(c *gin.Context, dst any) error {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return apperr.BadRequest("Content-Type must be application/json")
	}
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes))
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apperr.RequestTooLarge(MaxBodyBytes)
		}
		return apperr.BadRequest("Request body must be a single JSON value")
	}
	return validate(dst)
}

func decodeError(err error) error {
	var tooLarge *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooLarge):
		return apperr.RequestTooLarge(MaxBodyBytes)
	case errors.As(err, &typeErr) && typeErr.Field != "":
		return apperr.Validation([]apperr.FieldError{{Field: typeErr.Field, Rule: "type", Param: typeErr.Type.String()}})
	default:
		return apperr.BadRequest("Request body is not valid JSON")
	}
}

// BindQuery decodes the query string into dst using `form` tags and validates it.
func BindQuery(c *gin.Context, dst any) error {
	if err := binding.MapFormWithTag(dst, c.Request.URL.Query(), "form"); err != nil {
		return apperr.BadRequest("Malformed query parameters")
	}
	return validate(dst)
}

// ParamUUID reads a path parameter that must be a UUID. A malformed id is reported as a validation error rather than a lookup that cannot match.
func ParamUUID(c *gin.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, apperr.Validation([]apperr.FieldError{{Field: name, Rule: "uuid"}})
	}
	return id, nil
}
