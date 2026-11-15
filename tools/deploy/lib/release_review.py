"""Client and rehearsal release review over their existing request transports."""


def required(profile, status):
    return profile.get("schema") == 3 or "deployment_selection" in status.get("instance", {})


def review(request, activation, supply_configuration):
    """Review once, supply requested secrets if needed, and bind a complete plan."""
    plan = request("release-plan", activation)
    if not plan["ok"]:
        configuration = supply_configuration(plan["required_service_secrets"])
        plan = request("release-plan", {**activation, "secrets": {"configuration": configuration}})
    if plan["ok"]:
        activation["plan_token"] = plan["plan_token"]
    return plan
