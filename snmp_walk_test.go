package main

import (
	"fmt"
	"testing"

	"github.com/gosnmp/gosnmp"
)

type mockSnmpClient struct {
	responses []*gosnmp.SnmpPacket
	idx       int
}

func (m *mockSnmpClient) GetBulk(oids []string, nonRepeaters uint8, maxRepetitions uint32) (*gosnmp.SnmpPacket, error) {
	if m.idx >= len(m.responses) {
		return nil, fmt.Errorf("no more responses")
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func TestBulkWalkMulti(t *testing.T) {
	mock := &mockSnmpClient{
		responses: []*gosnmp.SnmpPacket{
			{
				Variables: []gosnmp.SnmpPDU{
					{Name: OID_HCIN + ".1", Type: gosnmp.Counter64, Value: uint64(100)},
					{Name: OID_HCOUT + ".1", Type: gosnmp.Counter64, Value: uint64(200)},
					{Name: OID_HCIN + ".2", Type: gosnmp.Counter64, Value: uint64(300)},
					{Name: OID_HCOUT + ".2", Type: gosnmp.Counter64, Value: uint64(400)},
				},
			},
			{
				Variables: []gosnmp.SnmpPDU{
					{Name: OID_HCIN + ".3", Type: gosnmp.EndOfMibView, Value: nil},
					{Name: OID_HCOUT + ".3", Type: gosnmp.EndOfMibView, Value: nil},
				},
			},
		},
	}

	baseOIDs := []string{OID_HCIN, OID_HCOUT}
	res, err := bulkWalkMulti(mock, baseOIDs, 2)
	if err != nil {
		t.Fatalf("bulkWalkMulti failed: %v", err)
	}

	if len(res[OID_HCIN]) != 2 {
		t.Errorf("expected 2 HCIN entries, got %d", len(res[OID_HCIN]))
	}
	if res[OID_HCIN][1] != 100 {
		t.Errorf("expected HCIN.1 to be 100, got %d", res[OID_HCIN][1])
	}
	if res[OID_HCOUT][2] != 400 {
		t.Errorf("expected HCOUT.2 to be 400, got %d", res[OID_HCOUT][2])
	}
}
