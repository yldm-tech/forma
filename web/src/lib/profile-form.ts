/** What the profile form would change: the name (trimmed) and language through the profile endpoint, the address (trimmed and lowercased, as forma2's form transformed it) through an email change. */
export const profileChanges = (
  current: { name: string; email: string; locale: string },
  form: { name: string; email: string; locale: string }
): { profile: { name?: string; locale?: string } | null; email: string | null } => {
  const profile: { name?: string; locale?: string } = {};
  const name = form.name.trim();
  if (name !== current.name) profile.name = name;
  if (form.locale !== current.locale) profile.locale = form.locale;
  const email = form.email.trim().toLowerCase();
  return {
    profile: Object.keys(profile).length > 0 ? profile : null,
    email: email !== current.email.toLowerCase() ? email : null,
  };
};
