export enum TResponseErrorCodesEnum {
  ResponseSendingError = "ResponseSendingError",
  RecaptchaError = "RecaptchaError",
  InvalidDeviceError = "InvalidDeviceError",
  ResponseAlreadyCompleted = "ResponseAlreadyCompleted",
  // A survey that takes one response per verified email refused to create or finish a response: the address already finished one.
  ResponseEmailAlreadyUsed = "ResponseEmailAlreadyUsed",
  // Permanent 4xx rejection (not "already completed"): the item is dropped from the queue,
  // so this is non-retryable — distinct from the retryable ResponseSendingError.
  ResponseSendingErrorPermanent = "ResponseSendingErrorPermanent",
}
