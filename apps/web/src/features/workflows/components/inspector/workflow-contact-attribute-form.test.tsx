import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { Provider, createStore } from "jotai";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test } from "vite-plus/test";
import type { AttributeKey } from "../../../../lib/attribute-keys";
import { initI18n } from "../../../../lib/i18n";
import { contactKeys } from "../../../../lib/queries/contacts";
import { type SdkIdentity, workspaceKeys } from "../../../../lib/queries/workspaces";
import type { TWorkflowResource } from "../../api";
import type { TWorkflowSetContactAttributeActionNode } from "../../definition";
import { workflowEditorAtom } from "../../state/editor";
import { WorkflowContactAttributeForm } from "./workflow-contact-attribute-form";

// The set_contact_attribute inspector: which edits of a step that writes an SDK-protected attribute from the respondent's answers ask for a confirmation first.

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has the <dialog> element but not its modal methods; these stand in for what the Dialog reads of them.
  HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
});

afterEach(() => {
  cleanup();
});

const WORKSPACE_ID = "0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d00";

const attributeKey = (key: string): AttributeKey => ({
  id: `key-${key}`,
  createdAt: "2026-10-01T09:30:00Z",
  updatedAt: "2026-10-01T09:30:00Z",
  key,
  name: null,
  description: null,
  isUnique: false,
  type: "custom",
  dataType: "string",
  workspaceId: WORKSPACE_ID,
});

/** Renders the form for a step whose config the form's changes are applied to, as the inspector does; plan and seats are protected from the SDK, nps is not. */
const renderForm = (initial: TWorkflowSetContactAttributeActionNode["config"]) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(contactKeys.attributeKeys(WORKSPACE_ID), [
    attributeKey("plan"),
    attributeKey("seats"),
    attributeKey("nps"),
  ]);
  const identity: SdkIdentity = {
    hasSecret: true,
    required: false,
    protectedAttributeKeys: ["plan", "seats"],
  };
  queryClient.setQueryData(workspaceKeys.sdkIdentity(WORKSPACE_ID), identity);
  const store = createStore();
  store.set(workflowEditorAtom, {
    ...store.get(workflowEditorAtom),
    workflow: { id: "workflow-1", workspaceId: WORKSPACE_ID } as TWorkflowResource,
  });
  const saved: TWorkflowSetContactAttributeActionNode["config"][] = [];

  const Harness = () => {
    const [node, setNode] = useState<TWorkflowSetContactAttributeActionNode>({
      id: "step-1",
      type: "action",
      actionType: "set_contact_attribute",
      config: initial,
    } as TWorkflowSetContactAttributeActionNode);
    return (
      <WorkflowContactAttributeForm
        node={node}
        isEditable
        onChange={(next) => {
          saved.push(next.config);
          setNode(next);
        }}
      />
    );
  };

  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Provider store={store}>
          <Harness />
        </Provider>
      </QueryClientProvider>
    </I18nextProvider>
  );
  return { saved };
};

const keySelect = () => document.getElementById("workflow-contact-attribute-key") as HTMLSelectElement;
const confirmTitle = () => i18n.t("workspace.workflows.contact_attribute_protected_confirm_title");
const confirmButton = () => i18n.t("workspace.workflows.contact_attribute_protected_confirm_button");

test("moving a confirmed protected write to another protected attribute asks again", () => {
  const { saved } = renderForm({ attributeKey: "plan", value: "{{answers.q1}}" });
  expect(keySelect().value).toBe("plan");

  fireEvent.change(keySelect(), { target: { value: "seats" } });

  expect(screen.getByText(confirmTitle())).toBeTruthy();
  expect(
    screen.getByText(i18n.t("workspace.workflows.contact_attribute_protected_confirm_body", { key: "seats" }))
  ).toBeTruthy();
  expect(saved).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: confirmButton() }));
  expect(saved).toEqual([{ attributeKey: "seats", value: "{{answers.q1}}" }]);
  expect(keySelect().value).toBe("seats");
});

test("cancelling the confirmation keeps the attribute the step wrote", () => {
  const { saved } = renderForm({ attributeKey: "plan", value: "{{answers.q1}}" });

  fireEvent.change(keySelect(), { target: { value: "seats" } });
  fireEvent.click(screen.getByRole("button", { name: i18n.t("common.cancel") }));

  expect(saved).toEqual([]);
  expect(keySelect().value).toBe("plan");
});

test("editing the value of a confirmed protected write does not ask again", () => {
  const { saved } = renderForm({ attributeKey: "plan", value: "{{answers.q1}}" });

  fireEvent.change(document.getElementById("workflow-contact-attribute-value") as HTMLTextAreaElement, {
    target: { value: "{{answers.q2}}" },
  });

  expect(screen.queryByText(confirmTitle())).toBeNull();
  expect(saved).toEqual([{ attributeKey: "plan", value: "{{answers.q2}}" }]);
});

test("a protected attribute set from a choice id is saved without a confirmation", () => {
  const { saved } = renderForm({ attributeKey: "nps", value: "{{answers.q1.choiceId}}" });

  fireEvent.change(keySelect(), { target: { value: "plan" } });

  expect(screen.queryByText(confirmTitle())).toBeNull();
  expect(saved).toEqual([{ attributeKey: "plan", value: "{{answers.q1.choiceId}}" }]);
});
