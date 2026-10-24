import type { TContactAttributeKey } from "@forma/types/contact-attribute-key";
import type {
  TAttributeOperator,
  TBaseFilter,
  TBaseFilters,
  TSegmentFilter,
  TSegmentFilterValue,
} from "@forma/types/segment";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { formSegment, formSetSegment } from "../lib/editor-adapter";
import { handleAddFilter } from "./add-filter-modal";
import { SegmentEditor } from "./segment-editor";

// The segment editor shared by the survey editor's targeting card and the segments page, driven the way the segments page drives it: through a segment whose filters are a form's tree. These carry over what the segments page's own editor did (its tree tests are in lib/segment-tree.test.ts).

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // jsdom has no modal dialogs; showing one is setting its open attribute.
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const device = (
  id: string,
  deviceType: "phone" | "desktop",
  connector: "and" | "or" | null
): TBaseFilter => ({
  id: `node${id}`,
  connector,
  resource: {
    id,
    root: { type: "device", deviceType },
    qualifier: { operator: "equals" },
    value: deviceType,
  },
});

const group = (id: string, connector: "and" | "or" | null, nodes: TBaseFilters): TBaseFilter => ({
  id,
  connector,
  resource: nodes,
});

const keys: TContactAttributeKey[] = [
  {
    id: "k1",
    createdAt: new Date(0),
    updatedAt: new Date(0),
    key: "plan",
    name: "Plan",
    description: null,
    isUnique: false,
    type: "custom",
    dataType: "string",
    workspaceId: "ws",
  },
];

/** A form holding a tree, edited through the adapter the segments page uses; latest() is the tree after the last edit. */
const renderFormWithKeys = (
  initial: TBaseFilters,
  contactAttributeKeys: TContactAttributeKey[],
  { viewOnly = false }: { viewOnly?: boolean } = {}
) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("[]", { status: 200, headers: { "content-type": "application/json" } }))
  );
  const changed = vi.fn<(filters: TBaseFilters) => void>();
  const Form = () => {
    const [filters, setFilters] = useState(initial);
    const segment = formSegment({ segmentId: "s1", workspaceId: "ws", filters, linkedSurveyIds: [] });
    return (
      <SegmentEditor
        group={filters}
        segment={segment}
        segments={[]}
        contactAttributeKeys={contactAttributeKeys}
        setSegment={formSetSegment(segment, (next) => {
          changed(next);
          setFilters(next);
        })}
        viewOnly={viewOnly}
      />
    );
  };
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nextProvider i18n={i18n}>
        <Form />
      </I18nextProvider>
    </QueryClientProvider>
  );
  return { latest: () => changed.mock.lastCall?.[0] ?? initial };
};

const renderForm = (initial: TBaseFilters, { viewOnly = false }: { viewOnly?: boolean } = {}) =>
  renderFormWithKeys(initial, keys, { viewOnly });

const nodesOf = (tree: TBaseFilters, index: number) => tree[index]?.resource as TBaseFilters;
const filterOf = (tree: TBaseFilters, index: number) => tree[index]?.resource as TSegmentFilter;

describe("SegmentEditor", () => {
  test("a group's Add filter opens that group's modal only and adds the filter inside it", () => {
    const form = renderForm([
      group("g1", null, [device("a", "phone", null)]),
      group("g2", "and", [device("b", "desktop", null)]),
    ]);
    const [first] = screen.getAllByRole("button", { name: i18n.t("common.add_filter") });
    fireEvent.click(first!);
    // Each open modal lists the device filters; one modal is open.
    const phones = screen.getAllByTestId("filter-btn-device-phone");
    expect(phones).toHaveLength(1);
    fireEvent.click(phones[0]!);

    const tree = form.latest();
    expect(nodesOf(tree, 0)).toHaveLength(2);
    expect(nodesOf(tree, 1)).toHaveLength(1);
    expect(screen.queryAllByTestId("filter-btn-device-phone")).toHaveLength(0);
  });

  test("Add filter below on a group adds the filter after the group, not inside it", async () => {
    const form = renderForm([group("g1", null, [device("a", "phone", null)]), device("b", "desktop", "and")]);
    fireEvent.keyDown(screen.getByTestId("segment-editor-group-menu-trigger"), { key: "Enter" });
    fireEvent.click(
      await screen.findByRole("menuitem", { name: i18n.t("workspace.segments.add_filter_below") })
    );
    fireEvent.click(screen.getByTestId("filter-btn-device-phone"));

    const tree = form.latest();
    expect(tree).toHaveLength(3);
    expect(nodesOf(tree, 0)).toHaveLength(1);
    expect(filterOf(tree, 1).root).toEqual({ type: "device", deviceType: "phone" });
    expect(filterOf(tree, 2).root).toEqual({ type: "device", deviceType: "desktop" });
  });

  test("deleting the first filter leaves the next one first, without a connector", () => {
    const form = renderForm([device("a", "phone", null), device("b", "desktop", "and")]);
    fireEvent.click(screen.getAllByRole("button", { name: i18n.t("common.delete") })[0]!);
    const tree = form.latest();
    expect(tree).toHaveLength(1);
    expect(tree[0]?.connector).toBeNull();
    expect(filterOf(tree, 0).id).toBe("b");
  });

  test("Create group wraps the filter in a group of its own", async () => {
    const form = renderForm([device("a", "phone", null), device("b", "desktop", "and")]);
    const row = screen.getAllByRole("button", { name: i18n.t("common.actions") })[1]!;
    fireEvent.keyDown(row, { key: "Enter" });
    fireEvent.click(await screen.findByRole("menuitem", { name: i18n.t("workspace.segments.create_group") }));
    const tree = form.latest();
    expect(tree).toHaveLength(2);
    expect(nodesOf(tree, 1)).toHaveLength(1);
    expect((nodesOf(tree, 1)[0]!.resource as TSegmentFilter).id).toBe("b");
  });

  test("the connector toggles between and and or", () => {
    const form = renderForm([device("a", "phone", null), device("b", "desktop", "and")]);
    fireEvent.click(
      screen.getByRole("button", {
        name: i18n.t("workspace.segments.toggle_connector", {
          current: i18n.t("workspace.segments.connector_and"),
          next: i18n.t("workspace.segments.connector_or"),
        }),
      })
    );
    expect(form.latest()[1]?.connector).toBe("or");
  });

  test("a read-only tree cannot be changed", () => {
    renderForm([group("g1", null, [device("a", "phone", null)])], { viewOnly: true });
    for (const name of [i18n.t("common.add_filter"), i18n.t("common.delete"), i18n.t("common.actions")]) {
      for (const button of screen.getAllByRole("button", { name })) {
        expect((button as HTMLButtonElement).disabled).toBe(true);
      }
    }
  });

  test("an attribute filter on a key the workspace no longer has still names its key", () => {
    renderForm([
      {
        id: "n1",
        connector: null,
        resource: {
          id: "f1",
          root: { type: "attribute", contactAttributeKey: "legacyPlan" },
          qualifier: { operator: "equals" },
          value: "pro",
        },
      },
    ]);
    const trigger = screen.getByRole("combobox", { name: i18n.t("common.attributes") });
    expect(within(trigger).getByText("legacyPlan")).toBeTruthy();
  });

  // The controls of a filter row have no visible labels; each is named for what it sets, as the segments page's own editor named them.
  test("every control of a filter row has an accessible name", () => {
    const t = (key: string) => i18n.t(key);
    const attribute = (
      id: string,
      key: string,
      operator: TAttributeOperator,
      value: TSegmentFilterValue
    ): TBaseFilter => ({
      id: `node${id}`,
      connector: id === "1" ? null : "and",
      resource: { id, root: { type: "attribute", contactAttributeKey: key }, qualifier: { operator }, value },
    });
    renderFormWithKeys(
      [
        attribute("1", "plan", "contains", "pro"),
        attribute("2", "seats", "greaterThan", 3),
        attribute("3", "signup", "isOlderThan", { amount: 2, unit: "weeks" }),
        attribute("4", "signup", "isBetween", ["2026-01-02T00:00:00.000Z", "2026-03-04T00:00:00.000Z"]),
        attribute("5", "signup", "isSameDay", "2026-05-06T00:00:00.000Z"),
        {
          id: "node6",
          connector: "and",
          resource: {
            id: "6",
            root: { type: "person", personIdentifier: "userId" },
            qualifier: { operator: "equals" },
            value: "u1",
          },
        },
        device("7", "phone", "and"),
      ],
      [
        ...keys,
        { ...keys[0]!, id: "k2", key: "seats", name: "Seats", dataType: "number" },
        { ...keys[0]!, id: "k3", key: "signup", name: "Signup", dataType: "date" },
      ]
    );

    // Operators are named by the operator they show, not by a generic "Select".
    expect(screen.queryAllByRole("combobox", { name: t("common.select") })).toHaveLength(0);
    for (const operator of [
      t("workspace.segments.operator_contains"),
      ">",
      t("workspace.segments.operator_is_older_than"),
      t("workspace.segments.operator_is_between"),
      t("workspace.segments.operator_is_same_day"),
    ]) {
      expect(screen.getByRole("combobox", { name: operator })).toBeTruthy();
    }
    // The person filter's and the device filter's equals.
    expect(screen.getAllByRole("combobox", { name: "=" })).toHaveLength(2);
    expect(screen.getByRole("combobox", { name: t("common.person") })).toBeTruthy();

    // The text attribute's suggesting field, the number attribute's field and the person's value.
    const values = screen.getAllByRole("textbox", { name: t("workspace.segments.filter_value") });
    expect(values.map((input) => (input as HTMLInputElement).value)).toEqual(["pro", "3", "u1"]);
    expect(screen.getByRole("combobox", { name: t("workspace.segments.filter_value") })).toBeTruthy();

    expect(
      (screen.getByRole("spinbutton", { name: t("workspace.segments.number") }) as HTMLInputElement).value
    ).toBe("2");
    expect(screen.getByRole("combobox", { name: t("workspace.segments.period") })).toBeTruthy();

    // A date picker's name says which date it is and keeps the date it shows.
    const after = screen.getByRole("button", {
      name: new RegExp(`^${t("workspace.segments.operator_is_after")} .*2026`),
    });
    const before = screen.getByRole("button", {
      name: new RegExp(`^${t("workspace.segments.operator_is_before")} .*2026`),
    });
    expect(after).not.toBe(before);
    expect(
      screen.getByRole("button", {
        name: new RegExp(`^${t("workspace.segments.operator_is_same_day")} .*2026`),
      })
    ).toBeTruthy();
  });
});

describe("new filters", () => {
  const added = (options: Omit<Parameters<typeof handleAddFilter>[0], "onAddFilter" | "setOpen">) => {
    const onAddFilter = vi.fn();
    const setOpen = vi.fn();
    handleAddFilter({ ...options, onAddFilter, setOpen });
    return { filter: onAddFilter.mock.calls[0]?.[0] as TBaseFilter | undefined, closed: setOpen.mock.calls };
  };

  test("each kind gets forma2's defaults, joined with and, and the modal closes", () => {
    const text = added({ type: "attribute", contactAttributeKey: "plan", attributeDataType: "string" });
    expect(text.filter).toMatchObject({
      connector: "and",
      resource: {
        root: { type: "attribute", contactAttributeKey: "plan" },
        qualifier: { operator: "equals" },
        value: "",
      },
    });
    expect(text.closed).toEqual([[false]]);
    expect(
      added({ type: "attribute", contactAttributeKey: "signup", attributeDataType: "date" }).filter
    ).toMatchObject({
      resource: { qualifier: { operator: "isOlderThan" }, value: { amount: 1, unit: "days" } },
    });
    expect(added({ type: "person" }).filter).toMatchObject({
      resource: {
        root: { type: "person", personIdentifier: "userId" },
        qualifier: { operator: "equals" },
        value: "",
      },
    });
    expect(added({ type: "segment", segmentId: "s1" }).filter).toMatchObject({
      resource: {
        root: { type: "segment", segmentId: "s1" },
        qualifier: { operator: "userIsIn" },
        value: "s1",
      },
    });
    expect(added({ type: "device", deviceType: "phone" }).filter).toMatchObject({
      resource: {
        root: { type: "device", deviceType: "phone" },
        qualifier: { operator: "equals" },
        value: "phone",
      },
    });
    expect(added({ type: "surveyInteraction" }).filter).toMatchObject({
      resource: {
        qualifier: { operator: "haveSeen" },
        value: { surveyScope: "any", surveyIds: [], within: { amount: 1, unit: "months" } },
      },
    });
  });

  test("an attribute, segment or device filter needs the key, segment or device it is about", () => {
    for (const type of ["attribute", "segment", "device"] as const) {
      const { filter, closed } = added({ type });
      expect(filter).toBeUndefined();
      expect(closed).toEqual([]);
    }
  });

  test("the node and the filter get fresh, distinct cuid2 ids", () => {
    const { filter } = added({ type: "person" });
    expect(filter?.id).toMatch(/^[0-9a-z]+$/);
    expect((filter!.resource as TSegmentFilter).id).toMatch(/^[0-9a-z]+$/);
    expect(filter!.id).not.toBe((filter!.resource as TSegmentFilter).id);
  });
});
