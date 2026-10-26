export enum TResponseErrorCodesEnum {
  ResponseSendingError = "ResponseSendingError",
  RecaptchaError = "RecaptchaError",
  InvalidDeviceError = "InvalidDeviceError",
  ResponseAlreadyCompleted = "ResponseAlreadyCompleted",
  // A survey that takes one response per verified email refused a new response: the address already has one.
  ResponseEmailAlreadyUsed = "ResponseEmailAlreadyUsed",
  // Permanent 4xx rejection (not "already completed"): the item is dropped from the queue,
  // so this is non-retryable — distinct from the retryable ResponseSendingError.
  ResponseSendingErrorPermanent = "ResponseSendingErrorPermanent",
}
