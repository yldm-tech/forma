"use client";

import { ArrowUpRightIcon, ChevronRightIcon, LogOutIcon, UserCircleIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useTranslation } from "react-i18next";
import type { TUser } from "@forma/types/user";
import { cn } from "@/lib/cn";
import { useSignOut } from "@/modules/auth/hooks/use-sign-out";
import { ProfileAvatar } from "@/modules/ui/components/avatars";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/modules/ui/components/dropdown-menu";

interface UserDropdownProps {
  user: TUser;
  organizationId: string;
  publicDomain: string;
  isCollapsed?: boolean;
  isTextVisible?: boolean;
  className?: string;
  /**
   * Where this is rendered, which decides both the trigger's shape and which way the menu opens.
   *
   * `sidebar` is a full-width row with a top border, opening to the right — the shape the onboarding
   * and settings sidebars still use. `topBar` is the avatar alone in the header's right corner,
   * opening downward and right-aligned so it stays on screen.
   */
  placement?: "sidebar" | "topBar";
}

// The avatar/account trigger + menu (Account, Documentation, Share feedback, Log out) shown at the
// bottom of the sidebar. Extracted so the standalone org/account settings shell renders the same
// user menu the workspace navigation does — otherwise those routes have no way to log out, which
// strands billing-role users who get redirected straight into settings.
export const UserDropdown = ({
  user,
  organizationId,
  publicDomain,
  isCollapsed = false,
  isTextVisible = false,
  className,
  placement = "sidebar",
}: Readonly<UserDropdownProps>) => {
  const { t } = useTranslation();
  const router = useRouter();
  const { signOut: signOutWithAudit } = useSignOut({ id: user.id, email: user.email });

  const dropdownNavigation = [
    {
      label: t("common.account"),
      href: "/account/settings/profile",
      icon: UserCircleIcon,
    },
    {
      label: t("common.documentation"),
      href: "https://forma.ylam.ai/docs",
      target: "_blank",
      icon: ArrowUpRightIcon,
    },
  ];

  const isTopBar = placement === "topBar";
  // In the top bar the trigger is the avatar and nothing else: a full-width row with a top border
  // belongs to a sidebar, and would draw a line across the header here.
  const triggerClasses = cn(
    "text-left transition-colors duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-slate-500",
    isTopBar
      ? "rounded-full hover:opacity-80 focus-visible:ring-offset-2"
      : "w-full border-t px-3 py-3 hover:bg-slate-50 focus-visible:ring-inset",
    !isTopBar && isCollapsed ? "flex items-center justify-center" : "",
    className
  );
  const iconClasses =
    "flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-slate-100 text-slate-600";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild id="userDropdownTrigger" className={triggerClasses}>
        <button
          type="button"
          aria-label={isTopBar || isCollapsed ? t("common.account_settings") : undefined}
          className={cn(
            "flex items-center gap-3",
            isTopBar ? "justify-center" : "w-full",
            !isTopBar && isCollapsed && "justify-center"
          )}>
          <span className={iconClasses}>
            <ProfileAvatar userId={user.id} />
          </span>
          {!isTopBar && !isCollapsed && !isTextVisible && (
            <>
              <div className="grow overflow-hidden">
                <p
                  title={user?.email}
                  className="ph-no-capture -mb-0.5 truncate text-sm font-bold text-slate-700">
                  {user?.name ? <span>{user?.name}</span> : <span>{user?.email}</span>}
                </p>
                {/* The address, not the word "Account". Which account you are signed in as is the
                    question this row answers, and the label restating its own type answered nothing.
                    Falls back to the type when the name line is already showing the address. */}
                <p className="ph-no-capture truncate text-sm text-slate-500">
                  {user?.name && user?.email ? user.email : t("common.account")}
                </p>
              </div>
              <ChevronRightIcon className="size-4 shrink-0 text-slate-600" strokeWidth={1.5} />
            </>
          )}
        </button>
      </DropdownMenuTrigger>

      <DropdownMenuContent
        id="userDropdownInnerContentWrapper"
        side={isTopBar ? "bottom" : "right"}
        sideOffset={isTopBar ? 8 : 10}
        alignOffset={isTopBar ? 0 : 5}
        align="end">
        {dropdownNavigation.map((link) => (
          <Link
            href={link.href}
            target={link.target}
            className="flex w-full items-center"
            key={link.label}
            rel={link.target === "_blank" ? "noopener noreferrer" : undefined}>
            <DropdownMenuItem>
              <link.icon className="mr-2 size-4" strokeWidth={1.5} />
              {link.label}
            </DropdownMenuItem>
          </Link>
        ))}
        <DropdownMenuItem
          onClick={async () => {
            const loginUrl = `${publicDomain}/auth/login`;
            const route = await signOutWithAudit({
              reason: "user_initiated",
              redirectUrl: loginUrl,
              organizationId,
              redirect: false,
              callbackUrl: loginUrl,
              clearWorkspaceId: true,
            });
            router.push(route?.url || loginUrl);
          }}
          icon={<LogOutIcon className="mr-2 size-4" strokeWidth={1.5} />}>
          {t("common.logout")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
