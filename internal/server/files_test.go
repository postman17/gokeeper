package server

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/kmorozov/gophkeeper/internal/server/storage"
	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"
)

// fakeBlobStore is an in-memory BlobStore used by unit tests.
type fakeBlobStore struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func newFakeBlobStore() *fakeBlobStore {
	return &fakeBlobStore{blobs: make(map[string][]byte)}
}

func (f *fakeBlobStore) Put(_ context.Context, key string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blobs[key] = data
	return nil
}

func (f *fakeBlobStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.blobs[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeBlobStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.blobs, key)
	return nil
}

// sliceChunkSource serves a byte slice as upload chunks.
type sliceChunkSource struct {
	chunks [][]byte
	pos    int
}

func (s *sliceChunkSource) Next() ([]byte, error) {
	if s.pos >= len(s.chunks) {
		return nil, io.EOF
	}
	c := s.chunks[s.pos]
	s.pos++
	return c, nil
}

// collectingSink gathers download chunks.
type collectingSink struct {
	chunks [][]byte
}

func (c *collectingSink) SendChunk(chunk []byte) error {
	c.chunks = append(c.chunks, chunk)
	return nil
}

func TestSealAndOpenRoundTrip(t *testing.T) {
	dek := []byte("0123456789abcdef0123456789abcdef")
	chunks := [][]byte{
		bytes.Repeat([]byte("a"), 64*1024),
		bytes.Repeat([]byte("b"), 1000),
		{},
		bytes.Repeat([]byte("c"), 3),
	}
	var want []byte
	for _, c := range chunks {
		want = append(want, c...)
	}
	var buf bytes.Buffer
	n, err := sealStreamToBlob(dek, &sliceChunkSource{chunks: chunks}, &buf)
	if err != nil {
		t.Fatalf("sealStreamToBlob: %v", err)
	}
	if n != int64(len(want)) {
		t.Fatalf("size: want %d, got %d", len(want), n)
	}
	sink := &collectingSink{}
	if err := openBlobToStream(dek, bytes.NewReader(buf.Bytes()), sink); err != nil {
		t.Fatalf("openBlobToStream: %v", err)
	}
	var got []byte
	for _, c := range sink.chunks {
		got = append(got, c...)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("round-trip mismatch: want %d bytes, got %d", len(want), len(got))
	}
}

func TestOpenBlobWrongKey(t *testing.T) {
	dek := []byte("0123456789abcdef0123456789abcdef")
	var buf bytes.Buffer
	if _, err := sealStreamToBlob(dek, &sliceChunkSource{chunks: [][]byte{[]byte("hello")}}, &buf); err != nil {
		t.Fatalf("sealStreamToBlob: %v", err)
	}
	err := openBlobToStream([]byte("fedcba9876543210fedcba9876543210"), bytes.NewReader(buf.Bytes()), &collectingSink{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("want Internal, got %v", err)
	}
}

// fakeUploadStream drives UploadFile with a canned sequence of messages.
type fakeUploadStream struct {
	ctx     context.Context
	msgs    []*pb.UploadFileRequest
	pos     int
	closed  *pb.UploadFileResponse
	recvErr error
}

func (f *fakeUploadStream) Context() context.Context { return f.ctx }

func (f *fakeUploadStream) Recv() (*pb.UploadFileRequest, error) {
	if f.recvErr != nil {
		return nil, f.recvErr
	}
	if f.pos >= len(f.msgs) {
		return nil, io.EOF
	}
	m := f.msgs[f.pos]
	f.pos++
	return m, nil
}

func (f *fakeUploadStream) SendAndClose(resp *pb.UploadFileResponse) error {
	f.closed = resp
	return nil
}

// grpc.ServerStream stubs required by the generated interface.
func (f *fakeUploadStream) SetHeader(grpcmetadata.MD) error  { return nil }
func (f *fakeUploadStream) SendHeader(grpcmetadata.MD) error { return nil }
func (f *fakeUploadStream) SetTrailer(grpcmetadata.MD)       {}

func (f *fakeUploadStream) RecvMsg(m any) error { return nil }

func (f *fakeUploadStream) SendMsg(m any) error { return nil }

// fakeDownloadStream collects the messages produced by DownloadFile.
type fakeDownloadStream struct {
	ctx  context.Context
	msgs []*pb.DownloadFileResponse
}

func (f *fakeDownloadStream) Context() context.Context { return f.ctx }

func (f *fakeDownloadStream) Send(resp *pb.DownloadFileResponse) error {
	f.msgs = append(f.msgs, resp)
	return nil
}

func (f *fakeDownloadStream) SetHeader(grpcmetadata.MD) error  { return nil }
func (f *fakeDownloadStream) SendHeader(grpcmetadata.MD) error { return nil }
func (f *fakeDownloadStream) SetTrailer(grpcmetadata.MD)       {}

func (f *fakeDownloadStream) RecvMsg(m any) error { return nil }

func (f *fakeDownloadStream) SendMsg(m any) error { return nil }

func TestUploadAndDownloadFile(t *testing.T) {
	s, jwt, store, blobs := newTestService(t)
	ctx := context.Background()
	reg, err := s.Register(ctx, &pb.RegisterRequest{Login: "fileuser", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := reg.GetUserId()
	content := bytes.Repeat([]byte("x"), 150*1024)
	var chunks []*pb.UploadFileRequest
	chunks = append(chunks, &pb.UploadFileRequest{Data: &pb.UploadFileRequest_Meta{Meta: &pb.FileMeta{
		Name: "photo.bin", Meta: "holiday",
	}}})
	for len(content) > 0 {
		n := 64 * 1024
		if n > len(content) {
			n = len(content)
		}
		chunks = append(chunks, &pb.UploadFileRequest{Data: &pb.UploadFileRequest_Chunk{Chunk: content[:n]}})
		content = content[n:]
	}
	upload := &fakeUploadStream{ctx: authedCtx(t, jwt, userID), msgs: chunks}
	if err := s.UploadFile(upload); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	fileID := upload.closed.GetId()
	if fileID == "" {
		t.Fatal("expected non-empty file id")
	}
	file, err := store.GetFile(ctx, uuid.MustParse(userID), uuid.MustParse(fileID))
	if err != nil {
		t.Fatalf("storage GetFile: %v", err)
	}
	if file.Name != "photo.bin" || file.Meta != "holiday" {
		t.Fatalf("unexpected file record: %+v", file)
	}
	if file.Size != int64(150*1024) {
		t.Fatalf("size: want %d, got %d", 150*1024, file.Size)
	}
	if _, ok := blobs.blobs[file.S3Key]; !ok {
		t.Fatalf("expected blob stored at %s", file.S3Key)
	}

	download := &fakeDownloadStream{ctx: authedCtx(t, jwt, userID)}
	if err := s.DownloadFile(&pb.DownloadFileRequest{Id: fileID}, download); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if len(download.msgs) == 0 {
		t.Fatal("expected stream messages")
	}
	if meta := download.msgs[0].GetMeta(); meta == nil || meta.GetName() != "photo.bin" || meta.GetSize() != int64(150*1024) {
		t.Fatalf("unexpected meta message: %+v", download.msgs[0])
	}
	var got []byte
	for _, m := range download.msgs[1:] {
		got = append(got, m.GetChunk()...)
	}
	if !bytes.Equal(bytes.Repeat([]byte("x"), 150*1024), got) {
		t.Fatalf("downloaded content mismatch: got %d bytes", len(got))
	}
}

func TestDownloadFileForeign(t *testing.T) {
	s, jwt, _, _ := newTestService(t)
	ctx := context.Background()
	reg, err := s.Register(ctx, &pb.RegisterRequest{Login: "owner", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	upload := &fakeUploadStream{ctx: authedCtx(t, jwt, reg.GetUserId()), msgs: []*pb.UploadFileRequest{
		{Data: &pb.UploadFileRequest_Meta{Meta: &pb.FileMeta{Name: "secret.txt"}}},
		{Data: &pb.UploadFileRequest_Chunk{Chunk: []byte("top secret")}},
	}}
	if err := s.UploadFile(upload); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	other, err := s.Register(ctx, &pb.RegisterRequest{Login: "attacker", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register attacker: %v", err)
	}
	err = s.DownloadFile(&pb.DownloadFileRequest{Id: upload.closed.GetId()}, &fakeDownloadStream{ctx: authedCtx(t, jwt, other.GetUserId())})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestDeleteFile(t *testing.T) {
	s, jwt, store, blobs := newTestService(t)
	ctx := context.Background()
	reg, err := s.Register(ctx, &pb.RegisterRequest{Login: "deleter", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	upload := &fakeUploadStream{ctx: authedCtx(t, jwt, reg.GetUserId()), msgs: []*pb.UploadFileRequest{
		{Data: &pb.UploadFileRequest_Meta{Meta: &pb.FileMeta{Name: "gone.bin"}}},
		{Data: &pb.UploadFileRequest_Chunk{Chunk: []byte("data")}},
	}}
	if err := s.UploadFile(upload); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	fileID := upload.closed.GetId()
	file, err := store.GetFile(ctx, uuid.MustParse(reg.GetUserId()), uuid.MustParse(fileID))
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if _, err := s.DeleteFile(authedCtx(t, jwt, reg.GetUserId()), &pb.DeleteFileRequest{Id: fileID}); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if _, err := store.GetFile(ctx, uuid.MustParse(reg.GetUserId()), uuid.MustParse(fileID)); err != storage.ErrNotFound {
		t.Fatalf("expected record removed, got %v", err)
	}
	if _, ok := blobs.blobs[file.S3Key]; ok {
		t.Fatal("expected blob removed from store")
	}
	_, err = s.DeleteFile(authedCtx(t, jwt, reg.GetUserId()), &pb.DeleteFileRequest{Id: fileID})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound on second delete, got %v", err)
	}
}

func TestListFiles(t *testing.T) {
	s, jwt, _, _ := newTestService(t)
	ctx := context.Background()
	reg, err := s.Register(ctx, &pb.RegisterRequest{Login: "lister", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := reg.GetUserId()
	actx := authedCtx(t, jwt, userID)
	for _, name := range []string{"a.txt", "b.txt"} {
		upload := &fakeUploadStream{ctx: actx, msgs: []*pb.UploadFileRequest{
			{Data: &pb.UploadFileRequest_Meta{Meta: &pb.FileMeta{Name: name}}},
			{Data: &pb.UploadFileRequest_Chunk{Chunk: []byte(name)}},
		}}
		if err := s.UploadFile(upload); err != nil {
			t.Fatalf("UploadFile: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	resp, err := s.ListFiles(actx, &pb.ListFilesRequest{})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(resp.GetFiles()) != 2 {
		t.Fatalf("want 2 files, got %d", len(resp.GetFiles()))
	}
	for _, f := range resp.GetFiles() {
		if f.GetName() == "" || f.GetSize() == 0 {
			t.Fatalf("unexpected file info: %+v", f)
		}
	}
}

func TestUploadFileNoMeta(t *testing.T) {
	s, jwt, _, _ := newTestService(t)
	reg, err := s.Register(context.Background(), &pb.RegisterRequest{Login: "nometa", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	err = s.UploadFile(&fakeUploadStream{ctx: authedCtx(t, jwt, reg.GetUserId()), msgs: []*pb.UploadFileRequest{
		{Data: &pb.UploadFileRequest_Chunk{Chunk: []byte("data")}},
	}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", err)
	}
}

// File methods of fakeStorage (the struct itself lives in service_test.go).

func (f *fakeStorage) CreateFile(_ context.Context, file storage.File) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if file.ID == uuid.Nil {
		file.ID = uuid.New()
	}
	file.UpdatedAt = time.Now()
	f.files[file.ID] = file
	return file.ID, nil
}

func (f *fakeStorage) GetFile(_ context.Context, userID, id uuid.UUID) (storage.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	file, ok := f.files[id]
	if !ok || file.UserID != userID {
		return storage.File{}, storage.ErrNotFound
	}
	return file, nil
}

func (f *fakeStorage) ListFiles(_ context.Context, userID uuid.UUID) ([]storage.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var files []storage.File
	for _, file := range f.files {
		if file.UserID == userID {
			files = append(files, file)
		}
	}
	return files, nil
}

func (f *fakeStorage) DeleteFile(_ context.Context, userID, id uuid.UUID) (storage.File, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	file, ok := f.files[id]
	if !ok || file.UserID != userID {
		return storage.File{}, false, nil
	}
	delete(f.files, id)
	return file, true, nil
}
