# SPDX-License-Identifier: Apache-2.0

"""The JSON Schema validator this port writes its own of, by input class.

IT IS LOAD-BEARING RATHER THAN INCIDENTAL. Two obligations rest on it: the params
gate, which is the only thing between a caller and a walk running on garbage, and
the contract comparator, which tells a collector author at their own test time
what the client would otherwise tell them at the operator's registration.

THE KEYWORD REFUSAL IS THE ROW MOST WORTH READING. A validator that silently
ignored an assertion keyword it does not implement would under-validate exactly
the collector whose author reached for that keyword, and a silently weaker gate is
worse than none because it reads as one. Annotations are ignored BY NAME; anything
else raises.
"""

import unittest

from knowledge_collector.jsonschema import (
    ANNOTATION_KEYWORDS,
    ASSERTION_KEYWORDS,
    SchemaError,
    ValidationError,
    satisfies,
    validate,
)


class KeywordVocabularyTest(unittest.TestCase):
    def test_a_keyword_this_validator_does_not_implement_raises_naming_it(self):
        for keyword, value in (
            ("patternProperties", {"^x": {"type": "string"}}),
            ("propertyNames", {"type": "string"}),
            ("contains", {"type": "string"}),
            ("if", {"type": "object"}),
            ("dependentRequired", {"a": ["b"]}),
            ("unevaluatedProperties", False),
        ):
            with self.subTest(keyword):
                with self.assertRaises(SchemaError) as caught:
                    validate({}, {"type": "object", keyword: value})
                self.assertIn(keyword, str(caught.exception))
                self.assertIn("not implemented", str(caught.exception))

    def test_the_control_an_implemented_keyword_does_not_raise(self):
        validate({"a": 1}, {"type": "object", "required": ["a"], "properties": {"a": {"type": "integer"}}})

    def test_annotation_keywords_are_ignored_by_name(self):
        schema = {"type": "object"}
        for keyword in ANNOTATION_KEYWORDS:
            schema[keyword] = "anything"
        validate({}, schema)

    def test_the_two_vocabularies_do_not_overlap(self):
        self.assertEqual(ANNOTATION_KEYWORDS & ASSERTION_KEYWORDS, frozenset())

    def test_a_nested_unimplemented_keyword_raises_too(self):
        with self.assertRaises(SchemaError):
            validate({"a": {}}, {"type": "object", "properties": {"a": {"contains": {"type": "string"}}}})


class TypeTest(unittest.TestCase):
    ROWS = (
        ("null", None, [True, 0, "", [], {}]),
        ("boolean", True, [1, 0, "true", None]),
        ("string", "s", [1, None, [], {}]),
        ("array", [], [{}, "", None]),
        ("object", {}, [[], "", None]),
        ("integer", 3, [True, 3.5, "3", None]),
        ("number", 3.5, [True, "3", None]),
    )

    def test_each_type_admits_its_own_and_refuses_the_others(self):
        for name, good, bads in self.ROWS:
            with self.subTest(name):
                validate(good, {"type": name})
                for bad in bads:
                    with self.assertRaises(ValidationError, msg="%r must not satisfy %s" % (bad, name)):
                        validate(bad, {"type": name})

    def test_a_bool_is_never_an_integer_or_a_number(self):
        """PYTHON'S bool IS A SUBCLASS OF int, so a naive numeric arm admits
        `true` where a schema says integer. This is the row that catches it."""
        for name in ("integer", "number"):
            with self.subTest(name):
                with self.assertRaises(ValidationError):
                    validate(True, {"type": name})
                with self.assertRaises(ValidationError):
                    validate(False, {"type": name})

    def test_a_whole_float_satisfies_integer_as_json_schema_says_it_does(self):
        validate(3.0, {"type": "integer"})
        with self.assertRaises(ValidationError):
            validate(3.5, {"type": "integer"})

    def test_a_union_type_admits_any_member(self):
        validate(None, {"type": ["string", "null"]})
        validate("s", {"type": ["string", "null"]})
        with self.assertRaises(ValidationError):
            validate(1, {"type": ["string", "null"]})

    def test_an_unknown_type_name_is_a_schema_error_not_a_validation_one(self):
        with self.assertRaises(SchemaError):
            validate("s", {"type": "strang"})


class ObjectTest(unittest.TestCase):
    def test_a_missing_required_property_is_refused_naming_it(self):
        with self.assertRaises(ValidationError) as caught:
            validate({}, {"type": "object", "required": ["id"]})
        self.assertIn("id", str(caught.exception))

    def test_a_declared_property_of_the_wrong_type_is_refused_at_its_path(self):
        with self.assertRaises(ValidationError) as caught:
            validate({"id": 1}, {"type": "object", "properties": {"id": {"type": "string"}}})
        self.assertIn("#/id", str(caught.exception))

    def test_an_undeclared_property_is_admitted_unless_additional_properties_says_otherwise(self):
        validate({"extra": 1}, {"type": "object", "properties": {}})
        with self.assertRaises(ValidationError):
            validate({"extra": 1}, {"type": "object", "properties": {}, "additionalProperties": False})

    def test_additional_properties_as_a_schema_validates_the_extras(self):
        validate({"a": "s"}, {"type": "object", "additionalProperties": {"type": "string"}})
        with self.assertRaises(ValidationError):
            validate({"a": 1}, {"type": "object", "additionalProperties": {"type": "string"}})

    def test_the_property_count_bounds(self):
        validate({"a": 1}, {"type": "object", "minProperties": 1, "maxProperties": 1})
        with self.assertRaises(ValidationError):
            validate({}, {"type": "object", "minProperties": 1})
        with self.assertRaises(ValidationError):
            validate({"a": 1, "b": 2}, {"type": "object", "maxProperties": 1})


class ArrayTest(unittest.TestCase):
    def test_items_validates_every_element(self):
        validate([1, 2], {"type": "array", "items": {"type": "integer"}})
        with self.assertRaises(ValidationError) as caught:
            validate([1, "two"], {"type": "array", "items": {"type": "integer"}})
        self.assertIn("[1]", str(caught.exception))

    def test_prefix_items_validates_positionally_and_items_takes_the_rest(self):
        schema = {"type": "array", "prefixItems": [{"type": "string"}], "items": {"type": "integer"}}
        validate(["a", 1, 2], schema)
        with self.assertRaises(ValidationError):
            validate([1, 1], schema)
        with self.assertRaises(ValidationError):
            validate(["a", "b"], schema)

    def test_the_length_bounds_and_uniqueness(self):
        validate([1], {"type": "array", "minItems": 1, "maxItems": 1})
        with self.assertRaises(ValidationError):
            validate([], {"type": "array", "minItems": 1})
        with self.assertRaises(ValidationError):
            validate([1, 2], {"type": "array", "maxItems": 1})
        validate([1, 2], {"type": "array", "uniqueItems": True})
        with self.assertRaises(ValidationError):
            validate([1, 1], {"type": "array", "uniqueItems": True})

    def test_uniqueness_distinguishes_true_from_one(self):
        validate([True, 1], {"type": "array", "uniqueItems": True})

    def test_the_empty_array_is_the_boundary_and_satisfies_a_plain_array(self):
        validate([], {"type": "array", "items": {"type": "integer"}})


class ScalarConstraintTest(unittest.TestCase):
    def test_the_string_bounds_and_the_pattern(self):
        validate("ab", {"type": "string", "minLength": 2, "maxLength": 2, "pattern": "^a"})
        with self.assertRaises(ValidationError):
            validate("a", {"type": "string", "minLength": 2})
        with self.assertRaises(ValidationError):
            validate("abc", {"type": "string", "maxLength": 2})
        with self.assertRaises(ValidationError):
            validate("b", {"type": "string", "pattern": "^a"})

    def test_an_unusable_pattern_is_a_schema_error(self):
        with self.assertRaises(SchemaError):
            validate("a", {"type": "string", "pattern": "([unclosed"})

    def test_the_numeric_bounds(self):
        validate(5, {"type": "integer", "minimum": 5, "maximum": 5})
        with self.assertRaises(ValidationError):
            validate(4, {"type": "integer", "minimum": 5})
        with self.assertRaises(ValidationError):
            validate(6, {"type": "integer", "maximum": 5})
        with self.assertRaises(ValidationError):
            validate(5, {"type": "integer", "exclusiveMinimum": 5})
        with self.assertRaises(ValidationError):
            validate(5, {"type": "integer", "exclusiveMaximum": 5})

    def test_multiple_of(self):
        validate(6, {"type": "integer", "multipleOf": 3})
        with self.assertRaises(ValidationError):
            validate(7, {"type": "integer", "multipleOf": 3})
        with self.assertRaises(SchemaError):
            validate(6, {"type": "integer", "multipleOf": 0})

    def test_enum_and_const_distinguish_true_from_one(self):
        validate(1, {"enum": [1, 2]})
        with self.assertRaises(ValidationError):
            validate(True, {"enum": [1, 2]})
        validate("a", {"const": "a"})
        with self.assertRaises(ValidationError):
            validate("b", {"const": "a"})


class CombinatorTest(unittest.TestCase):
    def test_all_of_any_of_one_of_and_not(self):
        validate(5, {"allOf": [{"type": "integer"}, {"minimum": 5}]})
        with self.assertRaises(ValidationError):
            validate(4, {"allOf": [{"type": "integer"}, {"minimum": 5}]})
        validate("a", {"anyOf": [{"type": "string"}, {"type": "integer"}]})
        with self.assertRaises(ValidationError):
            validate([], {"anyOf": [{"type": "string"}, {"type": "integer"}]})
        validate("a", {"oneOf": [{"type": "string"}, {"type": "integer"}]})
        with self.assertRaises(ValidationError):
            validate(1, {"oneOf": [{"type": "integer"}, {"minimum": 0}]})
        validate("a", {"not": {"type": "integer"}})
        with self.assertRaises(ValidationError):
            validate(1, {"not": {"type": "integer"}})


class BooleanAndReferenceTest(unittest.TestCase):
    def test_the_boolean_schema_forms(self):
        validate("anything", True)
        with self.assertRaises(ValidationError):
            validate("anything", False)

    def test_a_local_reference_resolves(self):
        schema = {"$defs": {"name": {"type": "string"}}, "type": "object", "properties": {"a": {"$ref": "#/$defs/name"}}}
        validate({"a": "s"}, schema)
        with self.assertRaises(ValidationError):
            validate({"a": 1}, schema)

    def test_a_reference_to_nothing_is_a_schema_error(self):
        with self.assertRaises(SchemaError):
            validate({}, {"$ref": "#/$defs/absent"})

    def test_a_remote_reference_is_refused_rather_than_fetched(self):
        with self.assertRaises(SchemaError) as caught:
            validate({}, {"$ref": "https://example.invalid/s.json"})
        self.assertIn("local reference", str(caught.exception))

    def test_a_schema_that_is_not_an_object_or_boolean_is_a_schema_error(self):
        with self.assertRaises(SchemaError):
            validate({}, ["not", "a", "schema"])


class ComparatorTest(unittest.TestCase):
    """The comparator asks a different question from the validator: does an
    ADVERTISED schema declare at least what a CONTRACT declares."""

    def test_a_contract_of_nothing_admits_anything(self):
        satisfies(None, {"type": "string"}, "#")

    def test_a_missing_advertised_schema_is_refused_where_the_contract_declares_one(self):
        with self.assertRaises(ValidationError):
            satisfies({"type": "object"}, None, "#")

    def test_a_stricter_advertised_schema_is_admitted(self):
        satisfies({"type": "object", "required": ["a"]}, {"type": "object", "required": ["a", "b"]}, "#")

    def test_an_advertised_schema_with_no_declared_type_is_named_as_such(self):
        with self.assertRaises(ValidationError) as caught:
            satisfies({"type": "object"}, {}, "#")
        self.assertIn("no declared type", str(caught.exception))

    def test_the_item_schema_is_compared_at_its_own_path(self):
        contract = {"type": "array", "items": {"type": "object", "required": ["id"]}}
        satisfies(contract, {"type": "array", "items": {"type": "object", "required": ["id"]}}, "out")
        with self.assertRaises(ValidationError) as caught:
            satisfies(contract, {"type": "array", "items": {"type": "object", "required": []}}, "out")
        self.assertIn("out[]", str(caught.exception))

    def test_a_non_object_schema_on_either_side_is_a_schema_error(self):
        with self.assertRaises(SchemaError):
            satisfies({"type": "object"}, "not a schema", "#")


if __name__ == "__main__":
    unittest.main()
