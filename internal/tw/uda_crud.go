package tw

import (
	"context"
	"fmt"
	"strings"
)

var validUDATypes = map[string]struct{}{
	"string":   {},
	"numeric":  {},
	"date":     {},
	"duration": {},
}

func (c *Client) CreateUDA(ctx context.Context, name, udaType, label, values string) error {
	return c.writeUDA(ctx, name, udaType, label, values)
}

func (c *Client) UpdateUDA(ctx context.Context, name, udaType, label, values string) error {
	return c.writeUDA(ctx, name, udaType, label, values)
}

func (c *Client) DeleteUDA(ctx context.Context, name string) error {
	if !UDANamePattern.MatchString(name) {
		return fmt.Errorf("%w: UDA name %q", ErrInvalid, name)
	}
	for _, key := range []string{"type", "label", "values"} {
		if err := c.Run(ctx, "config", "uda."+name+"."+key, ""); err != nil {
			return err
		}
	}
	c.udas.invalidate()
	return nil
}

func (c *Client) writeUDA(ctx context.Context, name, udaType, label, values string) error {
	if !UDANamePattern.MatchString(name) {
		return fmt.Errorf("%w: UDA name %q", ErrInvalid, name)
	}
	if _, ok := validUDATypes[udaType]; !ok {
		return fmt.Errorf("%w: UDA type %q must be one of string, numeric, date, duration", ErrInvalid, udaType)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("%w: UDA label is required", ErrInvalid)
	}
	values = strings.TrimSpace(values)
	if values != "" {
		parts := strings.Split(values, ",")
		for _, p := range parts {
			if strings.TrimSpace(p) == "" {
				return fmt.Errorf("%w: UDA values list contains an empty entry", ErrInvalid)
			}
		}
	}
	if err := c.Run(ctx, "config", "uda."+name+".type", udaType); err != nil {
		return err
	}
	if err := c.Run(ctx, "config", "uda."+name+".label", label); err != nil {
		return err
	}
	if err := c.Run(ctx, "config", "uda."+name+".values", values); err != nil {
		return err
	}
	c.udas.invalidate()
	return nil
}
