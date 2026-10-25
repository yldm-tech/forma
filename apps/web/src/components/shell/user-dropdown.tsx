import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { BellIcon, LogOutIcon, UserCircleIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { errorMessage } from "../../lib/errors";
import { type Me, signOut } from "../../lib/queries/auth";
import { toast } from "../../lib/toast";
import { ProfileAvatar } from "../ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";

/**
 * forma2's user menu: profile, notifications (left out for the billing role, which sees no survey activity) and log out. Logging out records why, forgets the remembered workspace and goes to the sign-in page.
 */
export const UserDropdown = ({
  user,
  organizationId,
  isBilling,
}: {
  user: Me;
  organizationId?: string;
  isBilling: boolean;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const logOut = async () => {
    try {
      await signOut(queryClient, { reason: "user_initiated", organizationId });
    } catch (error) {
      toast.error(errorMessage(t, error));
      return;
    }
    // Messages about the session just ended, such as a 2FA confirmation, must not follow the user onto the sign-in page.
    for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
    // The session is gone, so a page with unsaved edits has nothing left to keep editing: skip its "Discard changes?" question.
    await navigate({ href: "/auth/login", ignoreBlocker: true });
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={t("common.account_settings")}
          className="flex items-center justify-center rounded-full transition-colors duration-200 hover:opacity-80 focus-visible:ring-2 focus-visible:ring-slate-500 focus-visible:ring-offset-2 focus-visible:outline-none">
          <ProfileAvatar userId={user.id} name={user.name} />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="bottom" sideOffset={8} align="end">
        <div className="px-2 py-1.5">
          <p className="truncate text-sm font-bold text-slate-700">{user.name || user.email}</p>
          {user.name && <p className="truncate text-sm text-slate-500">{user.email}</p>}
        </div>
        <DropdownMenuItem asChild>
          <Link to="/account/settings/profile">
            <UserCircleIcon className="mr-2 size-4" strokeWidth={1.5} />
            {t("common.your_profile")}
          </Link>
        </DropdownMenuItem>
        {!isBilling && (
          <DropdownMenuItem asChild>
            <Link to="/account/settings/notifications">
              <BellIcon className="mr-2 size-4" strokeWidth={1.5} />
              {t("common.notifications")}
            </Link>
          </DropdownMenuItem>
        )}
        <DropdownMenuItem
          onSelect={() => void logOut()}
          icon={<LogOutIcon className="mr-2 size-4" strokeWidth={1.5} />}>
          {t("common.logout")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
