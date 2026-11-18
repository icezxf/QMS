package avscrape

import (
	"encoding/json"
	"testing"
)

// 回退方案探测的是 2MB 截断文件，验证真实文件大小被正确注入 format.size，
// 且 streams / duration / format_name 等下游需要的字段被保留。
func TestInjectRealFileSize修正截断文件大小(t *testing.T) {
	raw := []byte(`{
		"format": {"filename":"avprobe-123","format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"600.0","size":"2097152","bit_rate":"27962"},
		"streams": [{"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"tags":{"language":"und"}}]
	}`)

	const realSize int64 = 2 * 1024 * 1024 * 1024 // 2GB
	fixed := injectRealFileSize(raw, realSize)

	var data map[string]interface{}
	if err := json.Unmarshal(fixed, &data); err != nil {
		t.Fatalf("修正后的 JSON 无法解析：%v", err)
	}
	format := data["format"].(map[string]interface{})
	if format["size"] != "2147483648" {
		t.Errorf("size 未被修正为真实值：%v", format["size"])
	}
	// bit_rate 应基于真实 size 与 600s 时长重算：2GB*8/600 ≈ 28633115
	if format["bit_rate"] != "28633115" {
		t.Errorf("bit_rate 未按真实大小重算：%v", format["bit_rate"])
	}
	if format["format_name"] != "mov,mp4,m4a,3gp,3g2,mj2" || format["duration"] != "600.0" {
		t.Errorf("format_name/duration 被误改：%+v", format)
	}
	streams, ok := data["streams"].([]interface{})
	if !ok || len(streams) != 1 {
		t.Fatalf("streams 未保留：%+v", data["streams"])
	}
	st := streams[0].(map[string]interface{})
	if st["width"] != float64(1920) || st["codec_name"] != "h264" {
		t.Errorf("视频流信息被破坏：%+v", st)
	}
}

// 回退方案某些情况下 ffprobe 不输出 format 段，需补建后再写入 size。
func TestInjectRealFileSize缺失format段时补建(t *testing.T) {
	raw := []byte(`{"streams":[{"index":0,"codec_type":"video","codec_name":"h264"}]}`)
	fixed := injectRealFileSize(raw, 12345)

	var data map[string]interface{}
	if err := json.Unmarshal(fixed, &data); err != nil {
		t.Fatalf("修正后的 JSON 无法解析：%v", err)
	}
	format, ok := data["format"].(map[string]interface{})
	if !ok || format["size"] != "12345" {
		t.Errorf("缺失 format 段时未补建并写入 size：%+v", data["format"])
	}
	// 无 duration 时不应强行写 bit_rate
	if _, exists := format["bit_rate"]; exists {
		t.Errorf("无 duration 时不应写 bit_rate：%+v", format)
	}
}

// 非法 JSON 应原样返回，不破坏下游解析。
func TestInjectRealFileSize非法JSON原样返回(t *testing.T) {
	raw := []byte(`not a json`)
	fixed := injectRealFileSize(raw, 999)
	if string(fixed) != string(raw) {
		t.Errorf("非法 JSON 应原样返回，实际：%s", fixed)
	}
}
