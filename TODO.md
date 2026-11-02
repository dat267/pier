# Reproduced audit findings

- M122 (medium): pi-messages events take `contentIndex` from the wire without validation. A negative index panics in `setPiContent` (`message.Content[index]` below zero) and a huge one grows the content slice until memory is exhausted, so a broken or hostile pi-messages backend crashes the client. Pinned pi v1.5.0 rejects a non-integer or out-of-range index per event type (pi-messages.ts).
- M123 (low): a pi-messages `done` or `error` event's `responseModel` is dropped, so the message does not record the concrete model the backend served.
