/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func TestValidateTrustedCASecrets_OK(t *testing.T) {
	cas := []neo4jv1beta1.TrustedCASecret{
		{Name: "oidc-ca"},
		{Name: "ldap-ca", Key: "tls.crt"},
	}
	errs := ValidateTrustedCASecrets(cas, field.NewPath("spec", "trustedCASecrets"))
	assert.Empty(t, errs)
}

func TestValidateTrustedCASecrets_EmptyList(t *testing.T) {
	errs := ValidateTrustedCASecrets(nil, field.NewPath("spec", "trustedCASecrets"))
	assert.Empty(t, errs)
}

func TestValidateTrustedCASecrets_MissingName(t *testing.T) {
	cas := []neo4jv1beta1.TrustedCASecret{{Name: ""}}
	errs := ValidateTrustedCASecrets(cas, field.NewPath("spec", "trustedCASecrets"))
	if assert.Len(t, errs, 1) {
		assert.Equal(t, field.ErrorTypeRequired, errs[0].Type)
	}
}

func TestValidateTrustedCASecrets_DuplicateNames(t *testing.T) {
	cas := []neo4jv1beta1.TrustedCASecret{
		{Name: "shared"},
		{Name: "shared", Key: "different.crt"},
	}
	errs := ValidateTrustedCASecrets(cas, field.NewPath("spec", "trustedCASecrets"))
	if assert.Len(t, errs, 1) {
		assert.Equal(t, field.ErrorTypeInvalid, errs[0].Type)
		assert.Contains(t, errs[0].Detail, "doubles as the keytool alias")
	}
}

func TestValidateExtraVolumeMounts_OK(t *testing.T) {
	mounts := []corev1.VolumeMount{
		{Name: "plugin-jars", MountPath: "/var/lib/neo4j/products/custom-plugin"},
		{Name: "extra-config", MountPath: "/etc/neo4j-extra"},
	}
	errs := ValidateExtraVolumeMounts(mounts, field.NewPath("spec", "extraVolumeMounts"))
	assert.Empty(t, errs)
}

func TestValidateExtraVolumeMounts_ReservedPaths(t *testing.T) {
	cases := []string{
		"/data",
		"/data/",
		"/logs",
		"/conf",
		"/ssl",
		"/plugins",
		"/truststore",
		"/truststore-ca",
		"/var/lib/neo4j",
		"/var/lib/neo4j/data",
	}
	for _, p := range cases {
		mounts := []corev1.VolumeMount{{Name: "v", MountPath: p}}
		errs := ValidateExtraVolumeMounts(mounts, field.NewPath("spec", "extraVolumeMounts"))
		if assert.Len(t, errs, 1, "path %q should be reserved", p) {
			assert.Equal(t, field.ErrorTypeForbidden, errs[0].Type, "path %q", p)
		}
	}
}

func TestValidateExtraVolumeMounts_DuplicatePaths(t *testing.T) {
	mounts := []corev1.VolumeMount{
		{Name: "a", MountPath: "/extra"},
		{Name: "b", MountPath: "/extra"},
	}
	errs := ValidateExtraVolumeMounts(mounts, field.NewPath("spec", "extraVolumeMounts"))
	if assert.Len(t, errs, 1) {
		assert.Equal(t, field.ErrorTypeInvalid, errs[0].Type)
	}
}

func TestValidateExtraVolumeMounts_MissingFields(t *testing.T) {
	mounts := []corev1.VolumeMount{{}, {Name: "v"}}
	errs := ValidateExtraVolumeMounts(mounts, field.NewPath("spec", "extraVolumeMounts"))
	// First entry: missing both name and mountPath → 2 errors
	// Second entry: missing mountPath → 1 error
	assert.Len(t, errs, 3)
}

func TestValidateExtraVolumes_OK(t *testing.T) {
	vols := []corev1.Volume{
		{Name: "plugin-jars"},
		{Name: "extra-config"},
	}
	errs := ValidateExtraVolumes(vols, field.NewPath("spec", "extraVolumes"))
	assert.Empty(t, errs)
}

func TestValidateExtraVolumes_DuplicateName(t *testing.T) {
	vols := []corev1.Volume{
		{Name: "shared"},
		{Name: "shared"},
	}
	errs := ValidateExtraVolumes(vols, field.NewPath("spec", "extraVolumes"))
	if assert.Len(t, errs, 1) {
		assert.Equal(t, field.ErrorTypeInvalid, errs[0].Type)
	}
}

func TestValidateExtraVolumes_EmptyName(t *testing.T) {
	vols := []corev1.Volume{{}}
	errs := ValidateExtraVolumes(vols, field.NewPath("spec", "extraVolumes"))
	if assert.Len(t, errs, 1) {
		assert.Equal(t, field.ErrorTypeRequired, errs[0].Type)
	}
}
