export type Types = {
  Integer: { Read: number; Write: number };
  "Short Text": { Read: string; Write: string };
  Text: { Read: string; Write: string };
  "Date Time": { Read: string; Write: string };
  JSON: { Read: object; Write: object };
  Real: { Read: number; Write: number };
  Hashed: { Read: string; Write: string };
  WebResource: { Read: object; Write: object };
  Boolean: { Read: boolean; Write: boolean };
  Fancy: { Read: string; Write: string };
  Date: { Read: string; Write: string };
};

export interface Organization {
  Read: {
    id: Types["Integer"]["Read"];
    name: Types["Short Text"]["Read"];
  };
  Write: {
    name: Types["Short Text"]["Write"];
  };
}

export interface Release {
  Read: {
    id: Types["Integer"]["Read"];
    commit: Types["Short Text"]["Read"];
  };
  Write: {
    commit: Types["Short Text"]["Write"];
  };
}

export interface Application {
  Read: {
    id: Types["Integer"]["Read"];
    created_at: Types["Date Time"]["Read"];
    organization: { __id: Organization["Read"]["id"] };
    should_be_running__release: { __id: Release["Read"]["id"] } | null;
    is_of__class: "fleet" | "block" | "app";
    composition: Types["JSON"]["Read"];
    ratio: Types["Real"]["Read"];
    secret_hash: Types["Hashed"]["Read"];
    logo: Types["WebResource"]["Read"];
    weird: Types["Fancy"]["Read"];
    due_on: Types["Date"]["Read"];
    includes__SLA: Types["Short Text"]["Read"];
    provider: Types["Short Text"]["Read"];
    /** @experimental not stable yet */
    can_x: boolean;
    plain: boolean;
    owns__device?: Array<{ __id: number }>;
  };
  Write: {
    created_at: Types["Date Time"]["Write"];
    organization: { __id: Organization["Read"]["id"] };
    is_of__class: "fleet" | "block" | "app";
  };
}

export interface UserHasPublicKey {
  Read: {
    id: Types["Integer"]["Read"];
    title: Types["Short Text"]["Read"];
  };
  Write: {
    title: Types["Short Text"]["Write"];
  };
}

export interface PublicThing {
  Read: {
    label: Types["Short Text"]["Read"];
  };
  Write: {
    label: Types["Short Text"]["Write"];
  };
}

export interface Role {
  Read: {
    id: Types["Integer"]["Read"];
    name: Types["Short Text"]["Read"];
  };
  Write: Record<string, never>;
}

export default interface $Model {
  organization: Organization;
  release: Release;
  application: Application;
  user__has__public_key: UserHasPublicKey;
  public_thing: PublicThing;
  role: Role;

  // Synonyms
  user_public_key: UserHasPublicKey;
}
