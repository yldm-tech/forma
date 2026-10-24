/**
 * @vitest-environment jsdom
 */
import { cleanup, render, screen } from "@testing-library/react";
import * as React from "react";
import { afterEach, describe, expect, test } from "vite-plus/test";
import { FormField, type FormFieldProps } from "./form-field";

afterEach(() => {
  cleanup();
});

const fields: FormFieldProps["fields"] = [
  { id: "firstName", label: "First Name", required: true },
  { id: "lastName", label: "Last Name", required: true },
  { id: "email", label: "Email", required: true },
];

const renderContact = (props: Partial<FormFieldProps>) =>
  render(
    React.createElement(FormField, {
      elementId: "contact",
      headline: "How can we reach you?",
      fields,
      value: { firstName: "A" },
      onChange: () => undefined,
      required: true,
      ...props,
    })
  );

describe("FormField errors", () => {
  // The element's message used to sit above the first field even when that field was filled, repeating what the failing fields already said under themselves.
  test("with per-field messages, only the failing fields carry text and the element keeps its bar", () => {
    const { container } = renderContact({
      errorMessage: "Please fill out this field",
      fieldErrors: { lastName: "Please fill out this field", email: "Please enter a valid email address" },
    });

    expect(container.querySelector("#contact-error")?.textContent).toBe("");
    expect(container.querySelector(".bg-destructive")).not.toBeNull();
    expect(screen.getByLabelText("First Name*").getAttribute("aria-invalid")).toBeNull();
    expect(screen.getByLabelText("Last Name*").getAttribute("aria-invalid")).toBe("true");
    expect(container.querySelector("#contact-lastName-error")?.textContent).toBe(
      "Please fill out this field"
    );
    expect(container.querySelector("#contact-email-error")?.textContent).toBe(
      "Please enter a valid email address"
    );
    expect(screen.getAllByText("Please fill out this field")).toHaveLength(1);
  });

  test("without per-field messages, the element's message shows above the fields", () => {
    const { container } = renderContact({ errorMessage: "Please fill out this field" });

    expect(container.querySelector("#contact-error")?.textContent).toBe("Please fill out this field");
    for (const label of ["First Name*", "Last Name*", "Email*"]) {
      expect(screen.getByLabelText(label).getAttribute("aria-invalid")).toBe("true");
    }
  });
});
