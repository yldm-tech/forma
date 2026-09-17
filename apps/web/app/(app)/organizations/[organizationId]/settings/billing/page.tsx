import { PricingPage } from "@/modules/billing/page";

const Page = (props: Readonly<{ params: Promise<{ organizationId: string }> }>) => {
  return PricingPage(props);
};

export default Page;
