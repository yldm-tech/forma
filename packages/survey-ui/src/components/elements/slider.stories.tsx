import type { Meta, StoryObj } from "@storybook/react";
import {
  type BaseStylingOptions,
  type LabelStylingOptions,
  commonArgTypes,
  createCSSVariablesDecorator,
  createStatefulRender,
  elementStylingArgTypes,
  labelStylingArgTypes,
  surveyStylingArgTypes,
} from "../../lib/story-helpers";
import { Slider, type SliderProps } from "./slider";

type StoryProps = SliderProps & Partial<BaseStylingOptions & LabelStylingOptions> & Record<string, unknown>;

const meta: Meta<StoryProps> = {
  title: "UI-package/Elements/Slider",
  component: Slider,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "A slider element. Respondents pick a number from min to max in steps of step; until they move it, the slider rests in the middle and reports no value.",
      },
    },
  },
  tags: ["autodocs"],
  argTypes: {
    ...commonArgTypes,
    min: { control: "number", description: "The smallest value", table: { category: "Content" } },
    max: { control: "number", description: "The largest value", table: { category: "Content" } },
    step: {
      control: "number",
      description: "The distance between two values",
      table: { category: "Content" },
    },
    value: { control: "number", description: "The answered value", table: { category: "State" } },
    lowerLabel: { control: "text", description: "Label for the lower end", table: { category: "Content" } },
    upperLabel: { control: "text", description: "Label for the upper end", table: { category: "Content" } },
  },
  render: createStatefulRender(Slider),
};

export default meta;
type Story = StoryObj<StoryProps>;

export const StylingPlayground: Story = {
  args: {
    elementId: "slider-1",
    inputId: "slider-input-1",
    headline: "How many hours a week do you use the product?",
    min: 0,
    max: 40,
    step: 1,
    lowerLabel: "Never",
    upperLabel: "Full time",
  },
  argTypes: {
    ...elementStylingArgTypes,
    ...labelStylingArgTypes,
    ...surveyStylingArgTypes,
  },
  decorators: [createCSSVariablesDecorator<StoryProps>()],
};

export const Default: Story = {
  args: {
    elementId: "slider-default",
    inputId: "slider-input-default",
    headline: "How likely are you to renew?",
    min: 0,
    max: 100,
    step: 5,
  },
};

export const Answered: Story = {
  args: {
    elementId: "slider-answered",
    inputId: "slider-input-answered",
    headline: "How likely are you to renew?",
    min: 0,
    max: 100,
    step: 5,
    value: 70,
  },
};

export const WithError: Story = {
  args: {
    elementId: "slider-error",
    inputId: "slider-input-error",
    headline: "How likely are you to renew?",
    min: 0,
    max: 100,
    step: 5,
    required: true,
    errorMessage: "Move the slider to choose a value",
  },
};
