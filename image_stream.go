// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"sync"

	"github.com/wintermi/sigma/internal/streamstate"
)

// ImageEventKind identifies the kind of provider-neutral image stream event.
type ImageEventKind string

const (
	// ImageEventKindStart marks the beginning of an image stream.
	ImageEventKindStart ImageEventKind = "start"
	// ImageEventKindPartial carries a partial generated image.
	ImageEventKindPartial ImageEventKind = "image_partial"
	// ImageEventKindImage carries a final generated image.
	ImageEventKindImage ImageEventKind = "image"
	// ImageEventKindDone marks the successful end of an image stream.
	ImageEventKindDone ImageEventKind = "done"
	// ImageEventKindError marks an image stream error.
	ImageEventKindError ImageEventKind = "error"
)

// ImageEvent is a provider-neutral image-generation stream event.
type ImageEvent struct {
	Kind          ImageEventKind   `json:"kind"`
	Image         *ImageInput      `json:"image,omitempty"`
	PartialImage  *ImageInput      `json:"partialImage,omitempty"`
	FinalImages   *AssistantImages `json:"finalImages,omitempty"`
	Usage         *Usage           `json:"usage,omitempty"`
	StopReason    StopReason       `json:"stopReason,omitempty"`
	Error         string           `json:"error,omitempty"`
	SequenceIndex *int             `json:"sequenceIndex,omitempty"`
}

// IsTerminal reports whether kind ends an image stream.
func (kind ImageEventKind) IsTerminal() bool {
	return kind == ImageEventKindDone || kind == ImageEventKindError
}

// IsTerminal reports whether event ends an image stream.
func (event ImageEvent) IsTerminal() bool {
	return event.Kind.IsTerminal()
}

// ImageStream is a single-consumer stream of ordered image provider events.
type ImageStream struct {
	producer *streamstate.Producer[ImageEvent, AssistantImages]
}

// ImageStreamWriter is the provider side of an ImageStream.
type ImageStreamWriter interface {
	Emit(context.Context, ImageEvent) error
	Done(context.Context, AssistantImages) error
	Error(context.Context, error, AssistantImages) error
	Close()
}

type imageStreamWriter struct {
	producer *streamstate.Producer[ImageEvent, AssistantImages]
	partial  *imagePartialAccumulator
}

// NewImageStream constructs an image stream and its provider-side writer.
func NewImageStream(ctx context.Context) (*ImageStream, ImageStreamWriter) {
	partial := newImagePartialAccumulator()
	producer := streamstate.NewProducer[ImageEvent, AssistantImages](ctx, streamEventBuffer, partial.cancelTerminal)
	return &ImageStream{producer: producer}, &imageStreamWriter{producer: producer, partial: partial}
}

func errorImageStream(ctx context.Context, err error, final AssistantImages) *ImageStream {
	stream, writer := NewImageStream(ctx)
	_ = writer.Error(ctx, err, final)
	return stream
}

// Events returns the ordered image stream events.
func (s *ImageStream) Events() <-chan ImageEvent {
	if s == nil || s.producer == nil {
		ch := make(chan ImageEvent)
		close(ch)
		return ch
	}
	return s.producer.Events()
}

// Done closes after Events closes.
func (s *ImageStream) Done() <-chan struct{} {
	if s == nil || s.producer == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.producer.Done()
}

// Err returns the terminal image stream error, if any.
func (s *ImageStream) Err() error {
	if s == nil || s.producer == nil {
		return nil
	}
	return s.producer.Err()
}

// Final returns the terminal image response, if the stream recorded one.
func (s *ImageStream) Final() (AssistantImages, bool) {
	if s == nil || s.producer == nil {
		return AssistantImages{}, false
	}
	return s.producer.Final()
}

// Close stops the image stream without waiting for a provider terminal event.
func (s *ImageStream) Close() {
	if s == nil || s.producer == nil {
		return
	}
	s.producer.Close()
}

func (s *ImageStream) abort(err error) {
	if s == nil || s.producer == nil {
		return
	}
	s.producer.Abort(err)
}

func (w *imageStreamWriter) Emit(ctx context.Context, event ImageEvent) error {
	if event.IsTerminal() {
		return &Error{
			Code:    ErrorInvalidStreamEvent,
			Message: "terminal image events must be sent with Done or Error",
		}
	}
	w.partial.apply(event)
	return mapStreamStateError(w.producer.Emit(ctx, event))
}

func (w *imageStreamWriter) Done(ctx context.Context, final AssistantImages) error {
	event := finalImageEvent(ImageEventKindDone, final, "")
	return mapStreamStateError(w.producer.Finish(ctx, streamstate.Terminal[ImageEvent, AssistantImages]{
		Event:    event,
		Final:    final,
		HasFinal: true,
	}))
}

func (w *imageStreamWriter) Error(ctx context.Context, err error, final AssistantImages) error {
	if final.StopReason == "" {
		final.StopReason = StopReasonError
	}
	code := ErrorStream
	if final.StopReason == StopReasonAborted {
		code = ErrorAborted
	}
	terminalErr := terminalError(code, err, "image stream error")
	event := finalImageEvent(ImageEventKindError, final, terminalErr.Error())
	return mapStreamStateError(w.producer.Finish(ctx, streamstate.Terminal[ImageEvent, AssistantImages]{
		Event:    event,
		Final:    final,
		HasFinal: true,
		Err:      terminalErr,
	}))
}

func (w *imageStreamWriter) Close() {
	w.producer.Close()
}

// CollectImages consumes stream until it receives a terminal event or ctx is canceled.
func CollectImages(ctx context.Context, stream *ImageStream) (AssistantImages, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	events := stream.Events()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return closedImageStreamResult(stream)
			}
			switch event.Kind { //nolint:exhaustive
			case ImageEventKindDone:
				return finalImagesFromEvent(stream, event), nil
			case ImageEventKindError:
				final := finalImagesFromEvent(stream, event)
				return final, imageErrorFromEvent(event, stream.Err())
			}
		case <-ctx.Done():
			stream.abort(ctx.Err())
			return closedImageStreamResult(stream)
		}
	}
}

type imagePartialAccumulator struct {
	mu     sync.Mutex
	images []ImageInput
}

func newImagePartialAccumulator() *imagePartialAccumulator {
	return &imagePartialAccumulator{}
}

func (a *imagePartialAccumulator) apply(event ImageEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if event.Kind == ImageEventKindImage && event.Image != nil {
		a.images = append(a.images, *event.Image)
	}
}

func (a *imagePartialAccumulator) cancelTerminal(err error) streamstate.Terminal[ImageEvent, AssistantImages] {
	terminalErr := terminalError(ErrorAborted, err, "image stream aborted")
	final := a.final()
	final.StopReason = StopReasonAborted
	return streamstate.Terminal[ImageEvent, AssistantImages]{
		Event:    finalImageEvent(ImageEventKindError, final, terminalErr.Error()),
		Final:    final,
		HasFinal: true,
		Err:      terminalErr,
	}
}

func (a *imagePartialAccumulator) final() AssistantImages {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AssistantImages{Images: append([]ImageInput(nil), a.images...)}
}

func finalImageEvent(kind ImageEventKind, final AssistantImages, message string) ImageEvent {
	finalCopy := final
	return ImageEvent{
		Kind:        kind,
		FinalImages: &finalCopy,
		Usage:       finalCopy.Usage,
		StopReason:  finalCopy.StopReason,
		Error:       message,
	}
}

func finalImagesFromEvent(stream *ImageStream, event ImageEvent) AssistantImages {
	if event.FinalImages != nil {
		return *event.FinalImages
	}
	if final, ok := stream.Final(); ok {
		return final
	}
	return AssistantImages{}
}

func imageErrorFromEvent(event ImageEvent, err error) error {
	if err != nil {
		return err
	}
	if event.Error == "" {
		return nil
	}
	return &Error{Code: ErrorStream, Message: event.Error}
}

func closedImageStreamResult(stream *ImageStream) (AssistantImages, error) {
	final, _ := stream.Final()
	if err := stream.Err(); err != nil {
		return final, err
	}
	return final, nil
}
