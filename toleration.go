package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

var errTolerationFormat = errors.New("error parsing toleration")

const tolerationFormatDesc = `
OP:KEY:VALUE:EFFECT:PERIOD
If fewer segments are specified, the remainder are treated as empty
Segments:
	OP: Required, one of 'Exists', 'Equal'
	KEY: Required unless OP is 'Exists'
	VALUE: Optional, should be empty if OP is 'Exists'
	EFFECT: Optional, one of 'NoSchedule', 'PreferNoSchedule' or 'NoExecute'
	PERIOD: Optional, must be in duration format, e.g. '1m30s'
`

func parseToleration(v string) (corev1.Toleration, error) {
	var t corev1.Toleration

	if v == "" {
		return t, fmt.Errorf("%w: empty value", errTolerationFormat)
	}

	parts := strings.Split(v, ":")
	for i, part := range parts {
		switch i {
		case 0:
			switch op := corev1.TolerationOperator(part); op {
			case corev1.TolerationOpExists, corev1.TolerationOpEqual:
				t.Operator = op
			default:
				return t, fmt.Errorf(`%w: invalid operator %q`, errTolerationFormat, op)
			}

		case 1:
			t.Key = part

		case 2:
			t.Value = part

		case 3:
			effect := corev1.TaintEffect(part)
			switch effect {
			case corev1.TaintEffectNoExecute, corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule:
			default:
				return t, fmt.Errorf(`%w: invalid effect %q`, errTolerationFormat, part)
			}

			t.Effect = effect

		case 4:
			dur, err := time.ParseDuration(part)
			if err != nil {
				return t, fmt.Errorf(`%w: invalid toleration period %q, must be valid duration (%s)`, errTolerationFormat, part, err.Error())
			}

			secs := int64(dur.Seconds())
			t.TolerationSeconds = &secs

		default:
			return t, fmt.Errorf(`%w: too many parts`, errTolerationFormat)
		}
	}

	return t, nil
}
