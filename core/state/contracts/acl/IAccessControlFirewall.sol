pragma solidity ^0.8.23;

interface IAccessControlFirewall {
    /// @notice Returns true if `subject` is permitted to call `target` with `data`.
    function isPermitted(address subject, address target, bytes calldata data) external view returns (bool);

    /// @notice Reverts if not permitted. Returns true if permitted.
    function checkPermittedOrRevert(address subject, address target, bytes calldata data) external view returns (bool);

    /// @notice Grants permission for a selector on a subject->target pair.
    function grantSelector(address subject, address target, bytes4 selector) external;

    /// @notice Revokes permission for a selector on a subject->target pair.
    function revokeSelector(address subject, address target, bytes4 selector) external;

    /// @notice Grants permission for all selectors to a specific target for a subject.
    function grantAnySelector(address subject, address target) external;

    /// @notice Revokes permission for all selectors flag to a specific target for a subject.
    function revokeAnySelector(address subject, address target) external;

    /// @notice Sets an optional calldata constraint for a specific selector.
    /// If mask.length == 0, clears the constraint.
    function setParamConstraint(
        address subject,
        address target,
        bytes4 selector,
        bytes calldata mask,
        bytes calldata value
    ) external;

    /// @notice Grants contract creation permission to a subject.
    function grantContractCreation(address subject) external;

    /// @notice Revokes contract creation permission from a subject.
    function revokeContractCreation(address subject) external;
}

