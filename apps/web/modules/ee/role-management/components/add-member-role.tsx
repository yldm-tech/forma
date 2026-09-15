"use client";

import { useMemo } from "react";
import { type Control, Controller } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { TOrganizationRole } from "@forma/types/memberships";
import { getAccessFlags } from "@/lib/membership/utils";
import { Label } from "@/modules/ui/components/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/modules/ui/components/select";
import { Muted, P } from "@/modules/ui/components/typography";

interface AddMemberRoleProps {
  control: Control<{ name: string; email: string; role: TOrganizationRole; teamIds: string[] }>;
  isAccessControlAllowed: boolean;
  isFormaCloud: boolean;
  membershipRole?: TOrganizationRole;
}

export function AddMemberRole({
  control,
  isAccessControlAllowed,
  isFormaCloud,
  membershipRole,
}: AddMemberRoleProps) {
  const { isMember, isOwner } = getAccessFlags(membershipRole);

  const { t } = useTranslation();

  const roles = useMemo(() => {
    let rolesArray = ["member"];

    if (isOwner) {
      rolesArray.push("manager", "owner");
      if (isFormaCloud) {
        rolesArray.push("billing");
      }
    }
    return rolesArray;
  }, [isOwner, isFormaCloud]);

  if (isMember) return null;

  const rolesDescription = {
    owner: t("workspace.settings.teams.owner_role_description"),
    manager: t("workspace.settings.teams.manager_role_description"),
    member: t("workspace.settings.teams.member_role_description"),
    billing: t("workspace.settings.teams.billing_role_description"),
  };

  return (
    <Controller
      control={control}
      name="role"
      render={({ field: { onChange, value } }) => (
        <div className="flex flex-col gap-y-2">
          <Label>{t("workspace.settings.teams.organization_role")}</Label>
          <Select
            defaultValue={isAccessControlAllowed ? "member" : "owner"}
            disabled={!isAccessControlAllowed}
            onValueChange={(v) => {
              onChange(v as TOrganizationRole);
            }}
            value={value}>
            <SelectTrigger className="capitalize">
              <SelectValue>
                <P>{value}</P>
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectGroup className="flex flex-col-reverse">
                {roles.map((role) => (
                  <SelectItem key={role} value={role}>
                    <P className="capitalize">{role}</P>
                    <Muted className="text-slate-500">
                      {(rolesDescription as Record<string, string>)[role]}
                    </Muted>
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
      )}
    />
  );
}
