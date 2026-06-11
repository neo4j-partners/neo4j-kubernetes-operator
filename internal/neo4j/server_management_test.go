/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package neo4j

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	uuidA = "25a7efc7-d063-44b8-bdee-f23357f89f01"
	uuidB = "135ad202-5405-4d3c-9822-df39f59b823c"
)

func TestValidateServerIDs(t *testing.T) {
	require.NoError(t, validateServerIDs([]string{uuidA, uuidB}))
	assert.Error(t, validateServerIDs(nil), "empty list rejected")
	assert.Error(t, validateServerIDs([]string{"server-3"}), "non-UUID rejected")
	// Injection attempt must be rejected, not escaped-and-run.
	assert.Error(t, validateServerIDs([]string{"' OR '1'='1"}))
	assert.Error(t, validateServerIDs([]string{uuidA + "'; DROP SERVER 'x"}))
}

func TestBuildDeallocateStatement(t *testing.T) {
	stmt, err := buildDeallocateStatement([]string{uuidA}, false)
	require.NoError(t, err)
	assert.Equal(t, "DEALLOCATE DATABASES FROM SERVER '"+uuidA+"'", stmt)

	stmt, err = buildDeallocateStatement([]string{uuidA, uuidB}, true)
	require.NoError(t, err)
	assert.Equal(t, "DRYRUN DEALLOCATE DATABASES FROM SERVER '"+uuidA+"', '"+uuidB+"'", stmt)

	_, err = buildDeallocateStatement([]string{"not-a-uuid"}, false)
	assert.Error(t, err)
	_, err = buildDeallocateStatement(nil, false)
	assert.Error(t, err)
}

func TestBuildDropServerStatement(t *testing.T) {
	stmt, err := buildDropServerStatement(uuidB)
	require.NoError(t, err)
	assert.Equal(t, "DROP SERVER '"+uuidB+"'", stmt)

	_, err = buildDropServerStatement("server-4")
	assert.Error(t, err)
}
