import { useQuery } from "@connectrpc/connect-query";
import { getCurrentUser } from "gen/users/v1/users-UserService_connectquery";
import { UnitSystem } from "gen/users/v1/users_pb";

/**
 * UseUnitSystem returns the units the signed in user reads amounts in,
 * from the user the app loads first, metric until it has them.
 */
export const useUnitSystem = (): UnitSystem => {
  const { data } = useQuery(getCurrentUser, {}, { staleTime: Infinity });
  const system = data?.user?.unitSystem ?? UnitSystem.METRIC;
  return system === UnitSystem.UNSPECIFIED ? UnitSystem.METRIC : system;
};
