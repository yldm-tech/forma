import { beforeEach, describe, expect, test, vi } from "vitest";
import { sendEmail } from "./index";

const { mockCreateTransport, mockSendMail, mockLoggerError } = vi.hoisted(() => {
  const sendMail = vi.fn();

  return {
    mockCreateTransport: vi.fn(() => ({ sendMail })),
    mockSendMail: sendMail,
    mockLoggerError: vi.fn(),
  };
});

vi.mock("nodemailer", () => ({
  createTransport: mockCreateTransport,
}));

vi.mock("@forma/logger", () => ({
  logger: {
    debug: vi.fn(),
    error: mockLoggerError,
    info: vi.fn(),
    warn: vi.fn(),
  },
}));

const emailData = {
  to: "owner@example.com",
  subject: "Subject",
  html: "<html>body</html>",
};

describe("sendEmail", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateTransport.mockImplementation(() => ({ sendMail: mockSendMail }));
  });

  // This message is persisted verbatim as a workflow run's user-visible failure reason, so a fixed
  // "Incorrect SMTP credentials" told the owner to re-enter working credentials after a timeout, a TLS
  // fault or a recipient rejection.
  test.each([
    ["a connection timeout", "Connection timeout"],
    ["a recipient rejection", "550 5.1.1 Recipient address rejected"],
  ])("surfaces the real SMTP failure for %s", async (_label, message) => {
    mockSendMail.mockRejectedValue(new Error(message));

    await expect(sendEmail(emailData)).rejects.toThrow(`Failed to send email: ${message}`);
    expect(mockLoggerError).toHaveBeenCalled();
  });

  test("falls back to a generic reason when the thrown value is not an Error", async () => {
    mockSendMail.mockRejectedValue("socket hang up");

    await expect(sendEmail(emailData)).rejects.toThrow("Failed to send email: Unknown SMTP error");
  });
});
