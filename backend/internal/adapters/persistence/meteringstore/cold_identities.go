package meteringstore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Readers share the node lock; the bounded cold transfer takes it exclusively.
// This closes the read-hot/move/read-cold gap without serializing live reports.
func lockIdentityReader(tx *gorm.DB, node uint) error {
	var r struct{ ID uint }
	return tx.Model(&model.Node{}).Select("id").Clauses(clause.Locking{Strength: "SHARE"}).Where("id=?", node).Find(&r).Error
}
func coldWeight(tx *gorm.DB, kind uint8, key string) ([]byte, error) {
	if v, ok := tx.Statement.Settings.Load(fmt.Sprintf("metering:weights:%d", kind)); ok {
		if weight, found := v.(map[string][]byte)[key]; found {
			return weight, nil
		}
	}
	if kind == coldTarget {
		var n uint64
		if _, e := fmt.Sscan(key, &n); e != nil {
			return nil, e
		}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], n)
		return b[:], nil
	}
	var p model.MeteringIdentityProfile
	cacheKey := fmt.Sprintf("metering:profile:%d", kind)
	if v, ok := tx.Statement.Settings.Load(cacheKey); ok {
		p = v.(model.MeteringIdentityProfile)
	} else {
		if e := tx.First(&p, "kind=?", kind).Error; e != nil {
			return nil, e
		}
		tx.Statement.Settings.Store(cacheKey, p)
	}
	if tx.Dialector.Name() == "sqlite" {
		switch p.Collation {
		case "BINARY":
			return []byte(key), nil
		case "NOCASE":
			b := []byte(key)
			originalLength := len(b)
			if nul := bytes.IndexByte(b, 0); nul >= 0 {
				b = b[:nul]
			}
			for i, c := range b {
				if c >= 'A' && c <= 'Z' {
					b[i] = c + 32
				}
			}
			var size [8]byte
			binary.BigEndian.PutUint64(size[:], uint64(originalLength))
			return append(size[:], b...), nil
		case "RTRIM":
			return []byte(strings.TrimRight(key, " ")), nil
		default:
			return nil, errColdIdentity
		}
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(p.Collation) {
		return nil, errColdIdentity
	}
	charset := strings.Split(p.Collation, "_")[0]
	expr := "CONVERT(? USING " + charset + ") COLLATE " + p.Collation
	if p.PadSpace {
		expr = "RTRIM(" + expr + ")"
	}
	var weight []byte
	if e := tx.Raw("SELECT WEIGHT_STRING("+expr+")", key).Row().Scan(&weight); e != nil {
		return nil, e
	}
	if len(weight) == 0 {
		return nil, errColdIdentity
	}
	return weight, nil
}
func coldBlockQuery(tx *gorm.DB, node uint, kind uint8, weight []byte) *gorm.DB {
	route := coldRoute(weight)
	terms := make([]string, 0, 33)
	args := []any{}
	for d := uint8(0); d <= 32; d++ {
		terms = append(terms, "(depth=? AND prefix=?)")
		args = append(args, d, coldPrefix(route, d))
	}
	return tx.Model(&model.MeteringColdBlock{}).Where("node_id=? AND kind=?", node, kind).Where(strings.Join(terms, " OR "), args...)
}
func readColdBlock(tx *gorm.DB, node uint, kind uint8, weight []byte, update bool, probe ...bool) (model.MeteringColdBlock, []coldIdentity, error) {
	var heads []struct {
		model.MeteringColdBlock `gorm:"embedded"`
		PayloadBytes            uint64
	}
	isProbe := len(probe) > 0 && probe[0]
	if isProbe {
		if h, ok := cachedColdNegative(tx, node, kind, weight); ok {
			return h, nil, nil
		}
	}
	q := coldBlockQuery(tx, node, kind, weight).Select("id,node_id,kind,depth,prefix,version,entry_count,raw_bytes,substr(sha256,1,65) AS sha256,substr(bloom,1,1025) AS bloom,substr(header_sha256,1,65) AS header_sha256,octet_length(payload) AS payload_bytes").Limit(2)
	if update && !isProbe {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if e := q.Find(&heads).Error; e != nil {
		return model.MeteringColdBlock{}, nil, e
	}
	if len(heads) == 0 {
		var root model.MeteringColdRoot
		q := tx.Where("node_id=? AND kind=?", node, kind).Find(&root)
		if q.Error != nil {
			return model.MeteringColdBlock{}, nil, q.Error
		}
		if q.RowsAffected > 0 {
			return model.MeteringColdBlock{}, nil, errColdIdentity
		}
		return model.MeteringColdBlock{}, nil, nil
	}
	if len(heads) != 1 || heads[0].PayloadBytes > coldBlockBytes+1024 {
		return model.MeteringColdBlock{}, nil, errColdIdentity
	}
	b := heads[0].MeteringColdBlock
	if !validColdHeader(b) {
		return b, nil, errColdIdentity
	}
	if isProbe {
		cacheColdHeader(tx, b)
		if !coldBloomMayContain(b.Bloom, weight) {
			return b, nil, nil
		}
		if update {
			return readColdBlock(tx, node, kind, weight, true)
		}
	}
	var body struct{ Payload []byte }
	if e := tx.Model(&model.MeteringColdBlock{}).Select("substr(payload,1,?) AS payload", coldBlockBytes+1025).Where("id=?", b.ID).Take(&body).Error; e != nil {
		return b, nil, e
	}
	b.Payload = body.Payload
	entries, e := decodeCold(b)
	return b, entries, e
}
func lookupColdIdentity(tx *gorm.DB, node uint, kind uint8, key string, update bool) (coldIdentity, bool, error) {
	present, e := coldKindPresent(tx, node, kind)
	if e != nil || !present {
		return coldIdentity{}, false, e
	}
	weight, e := coldWeight(tx, kind, key)
	if e != nil {
		return coldIdentity{}, false, e
	}
	_, entries, e := readColdBlock(tx, node, kind, weight, update, true)
	if e != nil {
		return coldIdentity{}, false, e
	}
	i := sort.Search(len(entries), func(i int) bool { return bytes.Compare(entries[i].Weight, weight) >= 0 })
	if i == len(entries) || !bytes.Equal(entries[i].Weight, weight) {
		return coldIdentity{}, false, nil
	}
	return entries[i], true, nil
}
func verifyColdWrite(tx *gorm.DB, b model.MeteringColdBlock, entries []coldIdentity) error {
	var stored model.MeteringColdBlock
	if e := tx.Select("id,node_id,kind,depth,prefix,version,entry_count,raw_bytes,substr(sha256,1,65) AS sha256,substr(bloom,1,1025) AS bloom,substr(header_sha256,1,65) AS header_sha256,substr(payload,1,?) AS payload", coldBlockBytes+1025).First(&stored, b.ID).Error; e != nil {
		return e
	}
	got, e := decodeCold(stored)
	if e != nil {
		return e
	}
	if len(got) != len(entries) {
		return errColdIdentity
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], entries[i]) {
			return errColdIdentity
		}
	}
	return nil
}
func writeColdPage(tx *gorm.DB, node uint, kind, depth uint8, prefix uint32, id uint, entries []coldIdentity) error {
	oldEntries := int64(0)
	if id != 0 {
		var old model.MeteringColdBlock
		if e := tx.Select("id,entry_count").First(&old, id).Error; e != nil {
			return e
		}
		oldEntries = int64(old.EntryCount)
	}
	raw, e := coldRaw(entries)
	if e != nil {
		return e
	}
	if len(entries) > coldBlockEntries || len(raw) > coldBlockBytes {
		if depth == 32 {
			return errors.New("cold identity directory exhausted; original authority retained")
		}
		if id != 0 {
			if e = tx.Delete(&model.MeteringColdBlock{}, id).Error; e != nil {
				return e
			}
			if e = adjustColdRoot(tx, node, kind, -1, -oldEntries); e != nil {
				return e
			}
		}
		left, right := []coldIdentity{}, []coldIdentity{}
		for _, r := range entries {
			if coldPrefix(coldRoute(r.Weight), depth+1) == prefix*2 {
				left = append(left, r)
			} else {
				right = append(right, r)
			}
		}
		if e = writeColdPage(tx, node, kind, depth+1, prefix*2, 0, left); e != nil {
			return e
		}
		return writeColdPage(tx, node, kind, depth+1, prefix*2+1, 0, right)
	}
	b, e := encodeCold(entries)
	if e != nil {
		return e
	}
	b.ID, b.NodeID, b.Kind, b.Depth, b.Prefix = id, node, kind, depth, prefix
	b.HeaderSHA256 = coldHeaderDigest(b)
	tx.Statement.Settings.Delete(fmt.Sprintf("metering:cold-headers:%d:%d", node, kind))
	if id == 0 {
		e = tx.Create(&b).Error
	} else {
		e = tx.Model(&b).Select("version", "entry_count", "raw_bytes", "sha256", "payload", "bloom", "header_sha256").Updates(&b).Error
	}
	if e != nil {
		return e
	}
	pagesDelta := int64(0)
	if id == 0 {
		pagesDelta = 1
	}
	if e = adjustColdRoot(tx, node, kind, pagesDelta, int64(len(entries))-oldEntries); e != nil {
		return e
	}
	return verifyColdWrite(tx, b, entries)
}
func putColdIdentity(tx *gorm.DB, node uint, kind uint8, entry coldIdentity) error {
	b, entries, e := readColdBlock(tx, node, kind, entry.Weight, true)
	if e != nil {
		return e
	}
	for _, r := range entries {
		if bytes.Equal(r.Weight, entry.Weight) {
			if reflect.DeepEqual(r, entry) {
				return nil
			}
			return ErrReportReceiptConflict
		}
	}
	entries = append(entries, entry)
	return writeColdPage(tx, node, kind, b.Depth, b.Prefix, b.ID, entries)
}

// A late higher cumulative report reopens its one floor once. The page rewrite
// is bounded to 256KiB, commits with the new hot floor, and is not per sample.
func takeColdFloor(tx *gorm.DB, node uint, key string) (bool, error) {
	weight, e := coldWeight(tx, coldFloor, key)
	if e != nil {
		return false, e
	}
	b, entries, e := readColdBlock(tx, node, coldFloor, weight, true)
	if e != nil {
		return false, e
	}
	for i, r := range entries {
		if bytes.Equal(r.Weight, weight) {
			entries = append(entries[:i], entries[i+1:]...)
			return true, writeColdPage(tx, node, coldFloor, b.Depth, b.Prefix, b.ID, entries)
		}
	}
	return false, nil
}
func coldReceipt(tx *gorm.DB, node uint, key string) (model.MeteringReportReceipt, bool, error) {
	r, ok, e := lookupColdIdentity(tx, node, coldReport, key, false)
	if e != nil || !ok {
		return model.MeteringReportReceipt{}, ok, e
	}
	if len(r.Numbers) != 2 || r.Numbers[1] == 0 {
		return model.MeteringReportReceipt{}, false, errColdIdentity
	}
	return model.MeteringReportReceipt{NodeID: node, ReportID: r.Key, ProtocolEndpointID: uint(r.Numbers[0]), TrafficRecordID: uint(r.Numbers[1])}, true, nil
}
func coldCheckpoint(tx *gorm.DB, node uint, key string) (model.FlowCursorCheckpoint, bool, error) {
	r, ok, e := lookupColdIdentity(tx, node, coldFloor, key, true)
	if e != nil || !ok {
		return model.FlowCursorCheckpoint{}, ok, e
	}
	if len(r.Numbers) != 7 || r.Numbers[6] > 1 {
		return model.FlowCursorCheckpoint{}, false, errColdIdentity
	}
	for _, n := range r.Numbers[2:6] {
		if n > math.MaxInt64 {
			return model.FlowCursorCheckpoint{}, false, errColdIdentity
		}
	}
	n := r.Numbers
	return model.FlowCursorCheckpoint{NodeID: node, FlowID: r.Key, ProtocolCredentialID: uint(n[0]), Revision: n[1], RawBytes: int64(n[2]), UploadBytes: int64(n[3]), DownloadBytes: int64(n[4]), UsedBytes: int64(n[5]), IsCompleted: n[6] == 1}, true, nil
}

// Coalesce one transfer batch per affected page. No per-report BLOB rewrite.
type coldTransferPage struct {
	b       model.MeteringColdBlock
	entries []coldIdentity
}

var errColdTransferBudget = errors.New("cold transfer batch read budget exceeded; retry a smaller batch")

const coldHeaderSelect = "id,node_id,kind,depth,prefix,version,entry_count,raw_bytes,substr(sha256,1,65) AS sha256,substr(bloom,1,1025) AS bloom,substr(header_sha256,1,65) AS header_sha256"

func putColdMany(tx *gorm.DB, node uint, kind uint8, incoming []coldIdentity) error {
	if len(incoming) == 0 {
		return nil
	}
	pages, targets, e := coldTransferPages(tx, node, kind, incoming)
	if e != nil {
		return e
	}
	for i, entry := range incoming {
		p := pages[targets[i]]
		same := false
		for _, old := range p.entries {
			if bytes.Equal(old.Weight, entry.Weight) {
				if !reflect.DeepEqual(old, entry) {
					return ErrReportReceiptConflict
				}
				same = true
				break
			}
		}
		if !same {
			p.entries = append(p.entries, entry)
		}
	}
	ids := make([]uint, 0, len(pages))
	for id := range pages {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		p := pages[id]
		if e = writeColdPage(tx, node, kind, p.b.Depth, p.b.Prefix, id, p.entries); e != nil {
			return e
		}
	}
	return nil
}
func coldTransferPages(tx *gorm.DB, node uint, kind uint8, in []coldIdentity) (map[uint]*coldTransferPage, []uint, error) {
	pages := map[uint]*coldTransferPage{}
	targets := make([]uint, len(in))
	var root model.MeteringColdRoot
	q := tx.Where("node_id=? AND kind=?", node, kind).Find(&root)
	if q.Error != nil {
		return nil, nil, q.Error
	}
	if q.RowsAffected == 0 {
		var b model.MeteringColdBlock
		q = tx.Select("id").Where("node_id=? AND kind=?", node, kind).Limit(1).Find(&b)
		if q.Error != nil {
			return nil, nil, q.Error
		}
		if q.RowsAffected > 0 {
			return nil, nil, errColdIdentity
		}
		pages[0] = &coldTransferPage{}
		return pages, targets, nil
	}
	if root.PageCount < 1 {
		return nil, nil, errColdIdentity
	}
	type head struct {
		model.MeteringColdBlock `gorm:"embedded"`
		PayloadBytes            uint64
	}
	var headers []head
	query := tx.Model(&model.MeteringColdBlock{}).Where("node_id=? AND kind=?", node, kind).Select(coldHeaderSelect + ",octet_length(payload) AS payload_bytes")
	if root.PageCount == 1 {
		query = query.Where("depth=0 AND prefix=0")
	} else {
		terms := []string{}
		args := []any{}
		for d := uint8(0); d <= 32; d++ {
			set := map[uint32]bool{}
			for _, e := range in {
				set[coldPrefix(coldRoute(e.Weight), d)] = true
			}
			prefixes := make([]uint32, 0, len(set))
			for p := range set {
				prefixes = append(prefixes, p)
			}
			slices.Sort(prefixes)
			terms = append(terms, "(depth=? AND prefix IN ?)")
			args = append(args, d, prefixes)
		}
		query = query.Where(strings.Join(terms, " OR "), args...)
	}
	if e := query.Limit(len(in) + 1).Find(&headers).Error; e != nil {
		return nil, nil, e
	}
	if len(headers) == 0 || len(headers) > len(in) {
		return nil, nil, errColdIdentity
	}
	material := uint64(0)
	ids := []uint{}
	for _, h := range headers {
		if !validColdHeader(h.MeteringColdBlock) || h.RawBytes > coldBlockBytes || h.PayloadBytes > coldBlockBytes+1024 {
			return nil, nil, errColdIdentity
		}
		material += uint64(h.RawBytes) + h.PayloadBytes
		if material > 8*1024*1024 {
			return nil, nil, errColdTransferBudget
		}
		ids = append(ids, h.ID)
		pages[h.ID] = &coldTransferPage{b: h.MeteringColdBlock}
	}
	for i, e := range in {
		matches := 0
		for _, h := range headers {
			if coldPrefix(coldRoute(e.Weight), h.Depth) == h.Prefix {
				matches++
				targets[i] = h.ID
			}
		}
		if matches != 1 {
			return nil, nil, errColdIdentity
		}
	}
	slices.Sort(ids)
	for start := 0; start < len(ids); start += 32 {
		var bodies []struct {
			ID      uint
			Payload []byte
		}
		if e := tx.Model(&model.MeteringColdBlock{}).Select("id,substr(payload,1,?) AS payload", coldBlockBytes+1025).Where("id IN ?", ids[start:min(start+32, len(ids))]).Find(&bodies).Error; e != nil {
			return nil, nil, e
		}
		if len(bodies) != min(32, len(ids)-start) {
			return nil, nil, errColdIdentity
		}
		for _, body := range bodies {
			p := pages[body.ID]
			p.b.Payload = body.Payload
			entries, e := decodeCold(p.b)
			if e != nil {
				return nil, nil, e
			}
			p.entries = entries
		}
	}
	return pages, targets, nil
}

func loadLedgerTarget(tx *gorm.DB, node, source uint, out *model.MeteringLedgerTarget) (bool, error) {
	q := tx.Where("source_id=?", source).Find(out)
	if q.Error != nil || q.RowsAffected > 0 {
		return q.RowsAffected > 0, q.Error
	}
	e, ok, err := lookupColdIdentity(tx, node, coldTarget, fmt.Sprint(source), false)
	if err != nil || !ok {
		return ok, err
	}
	if len(e.Numbers) != 3 {
		return false, errColdIdentity
	}
	*out = model.MeteringLedgerTarget{SourceID: source, NodeID: node, ProtocolEndpointID: uint(e.Numbers[0]), RecordID: uint(e.Numbers[1]), ResultID: uint(e.Numbers[2])}
	return true, nil
}

func coldFloorCandidates(tx *gorm.DB, node uint, keys []string) ([]model.FlowCursorCheckpoint, error) {
	present, e := coldKindPresent(tx, node, coldFloor)
	if e != nil || !present {
		return nil, e
	}
	// Seal/split takes the exclusive node lock. Reopening only removes entries;
	// page IDs remain stable while these readers own the shared node lock.
	ids := []uint{}
	seen := map[uint]bool{}
	possible := []string{}
	for _, key := range keys {
		w, e := coldWeight(tx, coldFloor, key)
		if e != nil {
			return nil, e
		}
		if _, ok := cachedColdNegative(tx, node, coldFloor, w); ok {
			continue
		}
		var heads []model.MeteringColdBlock
		if e = coldBlockQuery(tx, node, coldFloor, w).Select("id,node_id,kind,depth,prefix,version,entry_count,raw_bytes,substr(sha256,1,65) AS sha256,substr(bloom,1,1025) AS bloom,substr(header_sha256,1,65) AS header_sha256").Limit(2).Find(&heads).Error; e != nil {
			return nil, e
		}
		if len(heads) != 1 || !validColdHeader(heads[0]) {
			return nil, errColdIdentity
		}
		b := heads[0]
		cacheColdHeader(tx, b)
		if !coldBloomMayContain(b.Bloom, w) {
			continue
		}
		possible = append(possible, key)
		if !seen[b.ID] {
			seen[b.ID] = true
			ids = append(ids, b.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		var b model.MeteringColdBlock
		if e := tx.Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).First(&b, id).Error; e != nil {
			return nil, e
		}
	}
	rows := []model.FlowCursorCheckpoint{}
	known := map[string]bool{}
	for _, key := range possible {
		r, ok, e := coldCheckpoint(tx, node, key)
		if e != nil {
			return nil, e
		}
		if ok && !known[r.FlowID] {
			known[r.FlowID] = true
			rows = append(rows, r)
		}
	}
	return rows, nil
}

func coldKindPresent(tx *gorm.DB, node uint, kind uint8) (bool, error) {
	key := fmt.Sprintf("metering:cold-present:%d:%d", node, kind)
	_, transactional := tx.Statement.ConnPool.(gorm.TxCommitter)
	if transactional {
		if v, ok := tx.Statement.Settings.Load(key); ok {
			return v.(bool), nil
		}
	}
	var root model.MeteringColdRoot
	q := tx.Where("node_id=? AND kind=?", node, kind).Find(&root)
	if q.Error != nil {
		return false, q.Error
	}
	yes := q.RowsAffected > 0
	if yes {
		if root.PageCount < 1 || root.EntryCount < 0 {
			return false, errColdIdentity
		}
	} else {
		var b model.MeteringColdBlock
		q = tx.Select("id").Where("node_id=? AND kind=?", node, kind).Limit(1).Find(&b)
		if q.Error != nil {
			return false, q.Error
		}
		if q.RowsAffected > 0 {
			return false, errColdIdentity
		}
	}
	if transactional {
		tx.Statement.Settings.Store(key, yes)
	}
	return yes, nil
}
func adjustColdRoot(tx *gorm.DB, node uint, kind uint8, pages, entries int64) error {
	root := model.MeteringColdRoot{NodeID: node, Kind: kind}
	if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&root).Error; e != nil {
		return e
	}
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=? AND kind=?", node, kind).First(&root).Error; e != nil {
		return e
	}
	root.PageCount += pages
	root.EntryCount += entries
	if root.PageCount < 0 || root.EntryCount < 0 {
		return errColdIdentity
	}
	tx.Statement.Settings.Delete(fmt.Sprintf("metering:cold-present:%d:%d", node, kind))
	return tx.Model(&root).Select("page_count", "entry_count").Updates(&root).Error
}

func cachedColdNegative(tx *gorm.DB, node uint, kind uint8, weight []byte) (model.MeteringColdBlock, bool) {
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return model.MeteringColdBlock{}, false
	}
	key := fmt.Sprintf("metering:cold-headers:%d:%d", node, kind)
	if v, ok := tx.Statement.Settings.Load(key); ok {
		for _, h := range v.([]model.MeteringColdBlock) {
			if coldPrefix(coldRoute(weight), h.Depth) == h.Prefix && !coldBloomMayContain(h.Bloom, weight) {
				return h, true
			}
		}
	}
	return model.MeteringColdBlock{}, false
}
func cacheColdHeader(tx *gorm.DB, h model.MeteringColdBlock) {
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return
	}
	h.Payload = nil
	key := fmt.Sprintf("metering:cold-headers:%d:%d", h.NodeID, h.Kind)
	headers := []model.MeteringColdBlock{}
	if v, ok := tx.Statement.Settings.Load(key); ok {
		headers = v.([]model.MeteringColdBlock)
	}
	for _, old := range headers {
		if old.ID == h.ID {
			return
		}
	}
	if len(headers) >= 16 {
		headers = headers[1:]
	}
	headers = append(headers, h)
	tx.Statement.Settings.Store(key, headers)
}

// Weight computation is independent of row ownership. Precompute one bounded
// SQL batch before taking the node's short conversion fence; the actual source
// rows are still revalidated/locked before any transfer commits.
func prepareColdWeights(tx *gorm.DB, kind uint8, keys []string) error {
	if kind == coldTarget || tx.Dialector.Name() != "mysql" || len(keys) == 0 {
		return nil
	}
	var p model.MeteringIdentityProfile
	if e := tx.First(&p, "kind=?", kind).Error; e != nil {
		return e
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(p.Collation) {
		return errColdIdentity
	}
	expr := "CONVERT(? USING " + strings.Split(p.Collation, "_")[0] + ") COLLATE " + p.Collation
	if p.PadSpace {
		expr = "RTRIM(" + expr + ")"
	}
	terms := make([]string, 0, len(keys))
	args := []any{}
	for i, key := range keys {
		terms = append(terms, fmt.Sprintf("SELECT %d AS ordinal,WEIGHT_STRING(%s) AS weight", i, expr))
		args = append(args, key)
	}
	var rows []struct {
		Ordinal int
		Weight  []byte
	}
	if e := tx.Raw(strings.Join(terms, " UNION ALL "), args...).Scan(&rows).Error; e != nil {
		return e
	}
	if len(rows) != len(keys) {
		return errColdIdentity
	}
	weights := map[string][]byte{}
	for _, r := range rows {
		if r.Ordinal < 0 || r.Ordinal >= len(keys) || len(r.Weight) == 0 {
			return errColdIdentity
		}
		weights[keys[r.Ordinal]] = r.Weight
	}
	tx.Statement.Settings.Store(fmt.Sprintf("metering:weights:%d", kind), weights)
	return nil
}
