package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	original := SendMsg{
		ToUser:  "bob",
		Content: "Привет, мир! 🌍",
	}

	var buf bytes.Buffer
	if err := Encode(&buf, TypeSendMsg, original); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	msgType, raw, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if msgType != TypeSendMsg {
		t.Errorf("type: got %#x, want %#x", msgType, TypeSendMsg)
	}

	var got SendMsg
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ToUser != original.ToUser || got.Content != original.Content {
		t.Errorf("payload mismatch: got %+v, want %+v", got, original)
	}
}

func TestMagicBytesCheck(t *testing.T) {
	// Tamper with the magic bytes — decode must reject it
	buf := bytes.NewBuffer([]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00})
	_, _, err := Decode(buf)
	if err == nil {
		t.Error("expected error on bad magic bytes, got nil")
	}
}

func TestPayloadTooLarge(t *testing.T) {
	oversized := make([]byte, MaxPayload+1)
	var buf bytes.Buffer
	err := Encode(&buf, TypeSendMsg, json.RawMessage(oversized))
	if err == nil {
		t.Error("expected error for oversized payload, got nil")
	}
}

func TestHeaderSize(t *testing.T) {
	// Verify the encoded frame starts with the correct 7-byte header
	var buf bytes.Buffer
	if err := Encode(&buf, TypeAuthReq, AuthReq{Username: "alice", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if data[0] != Magic1 || data[1] != Magic2 {
		t.Errorf("wrong magic: %#x %#x", data[0], data[1])
	}
	if data[2] != byte(TypeAuthReq) {
		t.Errorf("wrong type byte: %#x", data[2])
	}
}
